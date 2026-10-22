// Native Windows PIDs must not be compared with Git Bash's emulated PIDs.
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

function powershell(source, env = process.env) {
  return execFileSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-EncodedCommand',
    Buffer.from(`$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; ${source}`, 'utf16le').toString('base64')],
  { encoding: 'utf8', timeout: 30000, windowsHide: true, env }).trim();
}

function snapshot(port = 0) {
  if (!Number.isInteger(Number(port)) || Number(port) < 0 || Number(port) > 65535) throw Error('Invalid port');
  return JSON.parse(powershell(`
    $processes = @(Get-CimInstance Win32_Process | ForEach-Object {
      $ticks = $_.CreationDate.ToUniversalTime().Ticks;
      @{pid=[int]$_.ProcessId; parent=[int]$_.ParentProcessId; birth=[string]($ticks - ($ticks % 10))}
    });
    $listeners = @();
    if (${Number(port)} -gt 0) {
      $listeners = @(Get-NetTCPConnection -State Listen | Where-Object LocalPort -eq ${Number(port)} |
        Select-Object -ExpandProperty OwningProcess -Unique);
    };
    @{processes=$processes; listeners=$listeners} | ConvertTo-Json -Depth 4 -Compress
  `));
}

export function sameProcess(expected, current) {
  return Boolean(expected && current && expected.pid === current.pid && expected.birth === current.birth);
}

export function owns(receipt, pid, processes) {
  const byPID = new Map(processes.map(p => [p.pid, p]));
  const target = byPID.get(pid);
  if (!target) return false;
  if ((receipt.listeners ?? []).some(p => sameProcess(p, target))) return true;
  if (!sameProcess(receipt.root, byPID.get(receipt.root.pid))) return false;
  const seen = new Set();
  let current = target;
  for (let depth = 0; current && depth < 64; depth++) {
    if (sameProcess(receipt.root, current)) return true;
    if (seen.has(current.pid)) return false;
    seen.add(current.pid);
    const parent = byPID.get(current.parent);
    // A recycled parent PID cannot establish ancestry.
    if (!parent || BigInt(parent.birth) > BigInt(current.birth)) return false;
    current = parent;
  }
  return false;
}

const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const readReceipt = file => JSON.parse(readFileSync(file, 'utf8'));
const saveReceipt = (file, receipt) => writeFileSync(file, JSON.stringify(receipt));

export function quoteWindowsArgument(value) {
  return '"' + String(value).replace(/(\\*)"/g, '$1$1\\"').replace(/(\\+)$/, '$1$1') + '"';
}

function launchHidden(input, log, env) {
  // DETACHED_PROCESS leaves native descendants free to allocate visible consoles.
  // Give the tree its own hidden console instead. Win32 launch also avoids
  // libuv's kill-on-parent-exit job, so the managed service outlives this helper.
  const payload = Buffer.from(JSON.stringify({ file: input[0],
    commandLine: input.map(quoteWindowsArgument).join(' '), log, cwd: process.cwd() })).toString('base64');
  return JSON.parse(powershell(`
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Globalization;
using System.Runtime.InteropServices;
using System.Text;
public static class HiddenDevProcess {
  [StructLayout(LayoutKind.Sequential)] public struct Security {
    public int length; public IntPtr descriptor; [MarshalAs(UnmanagedType.Bool)] public bool inherit;
  }
  [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)] public struct Startup {
    public int size; public string reserved, desktop, title;
    public uint x, y, width, height, xChars, yChars, fill, flags;
    public short show, reservedBytes; public IntPtr reservedPointer, input, output, error;
  }
  [StructLayout(LayoutKind.Sequential)] public struct StartupEx {
    public Startup info; public IntPtr attributes;
  }
  [StructLayout(LayoutKind.Sequential)] public struct ProcessInfo {
    public IntPtr process, thread; public uint pid, tid;
  }
  [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
  static extern IntPtr CreateFileW(string path, uint access, uint share, ref Security security, uint disposition, uint flags, IntPtr template);
  [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
  static extern bool CreateProcessW(string application, StringBuilder command, IntPtr processSecurity, IntPtr threadSecurity, bool inherit, uint flags, IntPtr environment, string directory, ref StartupEx startup, out ProcessInfo process);
  [DllImport("kernel32.dll", SetLastError=true)]
  static extern bool InitializeProcThreadAttributeList(IntPtr list, int count, int flags, ref IntPtr size);
  [DllImport("kernel32.dll", SetLastError=true)]
  static extern bool UpdateProcThreadAttribute(IntPtr list, uint flags, IntPtr attribute, IntPtr value, IntPtr size, IntPtr previous, IntPtr returned);
  [DllImport("kernel32.dll")] static extern void DeleteProcThreadAttributeList(IntPtr list);
  [DllImport("kernel32.dll", SetLastError=true)] static extern bool GetProcessTimes(IntPtr process, out long created, out long exited, out long kernel, out long user);
  [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
  [DllImport("kernel32.dll")] static extern bool TerminateProcess(IntPtr process, uint code);
  static void Check(bool success) { if (!success) throw new Win32Exception(Marshal.GetLastWin32Error()); }
  static readonly IntPtr Invalid = new IntPtr(-1);
  public static string Start(string application, string command, string log, string directory) {
    var security = new Security { length=Marshal.SizeOf(typeof(Security)), inherit=true };
    IntPtr output=Invalid, input=Invalid, attributes=IntPtr.Zero, handles=IntPtr.Zero;
    var process = new ProcessInfo();
    bool initialized=false, ready=false;
    try {
      output = CreateFileW(log, 0x40000000, 7, ref security, 2, 0x80, IntPtr.Zero);
      Check(output != Invalid);
      input = CreateFileW("NUL", 0x80000000, 7, ref security, 3, 0x80, IntPtr.Zero);
      Check(input != Invalid);
      IntPtr size=IntPtr.Zero;
      InitializeProcThreadAttributeList(IntPtr.Zero, 1, 0, ref size);
      attributes=Marshal.AllocHGlobal(size);
      Check(InitializeProcThreadAttributeList(attributes, 1, 0, ref size));
      initialized=true;
      handles=Marshal.AllocHGlobal(IntPtr.Size * 2);
      Marshal.WriteIntPtr(handles, 0, input);
      Marshal.WriteIntPtr(handles, IntPtr.Size, output);
      // Inherit only log/NUL handles, never the helper's stdout/stderr pipes.
      Check(UpdateProcThreadAttribute(attributes, 0, new IntPtr(0x20002), handles, new IntPtr(IntPtr.Size * 2), IntPtr.Zero, IntPtr.Zero));
      var startup = new StartupEx { attributes=attributes };
      startup.info.size=Marshal.SizeOf(typeof(StartupEx));
      startup.info.flags=0x101; startup.info.show=0;
      startup.info.input=input; startup.info.output=output; startup.info.error=output;
      // EXTENDED_STARTUPINFO_PRESENT | CREATE_NEW_CONSOLE | CREATE_NEW_PROCESS_GROUP
      Check(CreateProcessW(application, new StringBuilder(command), IntPtr.Zero, IntPtr.Zero, true, 0x80410, IntPtr.Zero, directory, ref startup, out process));
      long created, exited, kernel, user;
      Check(GetProcessTimes(process.process, out created, out exited, out kernel, out user));
      long ticks=DateTime.FromFileTimeUtc(created).Ticks;
      string result=process.pid.ToString(CultureInfo.InvariantCulture) + ":" + (ticks - ticks % 10).ToString(CultureInfo.InvariantCulture);
      ready=true;
      return result;
    } finally {
      if (!ready && process.process != IntPtr.Zero) TerminateProcess(process.process, 143);
      if (process.thread != IntPtr.Zero) CloseHandle(process.thread);
      if (process.process != IntPtr.Zero) CloseHandle(process.process);
      if (initialized) DeleteProcThreadAttributeList(attributes);
      if (attributes != IntPtr.Zero) Marshal.FreeHGlobal(attributes);
      if (handles != IntPtr.Zero) Marshal.FreeHGlobal(handles);
      if (input != Invalid) CloseHandle(input);
      if (output != Invalid) CloseHandle(output);
    }
  }
}
'@
    $launch = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('${payload}')) | ConvertFrom-Json;
    $application = (Get-Command -Name $launch.file -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source;
    $result = [HiddenDevProcess]::Start($application, $launch.commandLine, $launch.log, $launch.cwd).Split(':');
    @{pid=[int]$result[0]; birth=$result[1]} | ConvertTo-Json -Compress
  `, env));
}

function terminateIdentities(targets) {
  if (!targets.length) return;
  const encoded = Buffer.from(JSON.stringify(targets)).toString('base64');
  powershell(`
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class DevProcess {
  [DllImport("kernel32.dll", SetLastError=true)] public static extern IntPtr OpenProcess(uint access, bool inherit, int pid);
  [DllImport("kernel32.dll", SetLastError=true)] public static extern bool GetProcessTimes(IntPtr handle, out long created, out long exited, out long kernel, out long user);
  [DllImport("kernel32.dll", SetLastError=true)] public static extern bool TerminateProcess(IntPtr handle, uint code);
  [DllImport("kernel32.dll")] public static extern bool CloseHandle(IntPtr handle);
}
'@
        $targets = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('${encoded}')) | ConvertFrom-Json;
        foreach ($target in $targets) {
          # Query and terminate through one pinned native handle; never reopen a PID.
          $handle = [DevProcess]::OpenProcess(0x101001, $false, [int]$target.pid);
          if ($handle -eq [IntPtr]::Zero) { continue };
          try {
            [long]$created=0; [long]$exited=0; [long]$kernel=0; [long]$user=0;
            if ([DevProcess]::GetProcessTimes($handle, [ref]$created, [ref]$exited, [ref]$kernel, [ref]$user)) {
              $ticks = [DateTime]::FromFileTimeUtc($created).Ticks;
              if ([string]($ticks - ($ticks % 10)) -eq $target.birth) {
                $null = [DevProcess]::TerminateProcess($handle, 143);
              }
            }
          } finally { $null = [DevProcess]::CloseHandle($handle) }
        }
  `);
}

export async function run([command, file, ...args]) {
  if (process.platform !== 'win32') throw Error('Native Windows helper requires Windows');
  if (command === 'listener') {
    const { listeners } = snapshot(file);
    if (listeners.length > 1) throw Error('Multiple listener owners');
    return listeners[0] ?? '';
  }
  if (command === 'launch') {
    const [log, ...input] = args;
    const env = { ...process.env };
    // Desktop passes env assignments before make. Spawn make directly so its
    // native lifetime is the recorded root, independent of MSYS exec behavior.
    if (input[0] === 'env') {
      input.shift();
      while (/^[A-Za-z_][A-Za-z0-9_]*=/.test(input[0] ?? '')) {
        const assignment = input.shift();
        const separator = assignment.indexOf('=');
        env[assignment.slice(0, separator)] = assignment.slice(separator + 1);
      }
    }
    const root = launchHidden(input, log, env);
    try { saveReceipt(file, { root, listeners: [] }); }
    catch (error) { terminateIdentities([root]); throw error; }
    return root.pid;
  }
  if (!existsSync(file)) {
    if (command === 'stop') return '';
    throw Error('No native process receipt');
  }
  const receipt = readReceipt(file);
  if (command === 'alive') {
    const current = snapshot().processes.find(p => p.pid === receipt.root.pid);
    if (!sameProcess(receipt.root, current)) throw Error('Launcher is no longer alive');
    return current.pid;
  }
  if (command === 'owns' || command === 'record') {
    const pid = Number(args[0]);
    const { processes } = snapshot();
    if (!owns(receipt, pid, processes)) throw Error('Listener is not owned by this environment');
    if (command === 'record') {
      receipt.listeners = [...(receipt.listeners ?? []).filter(p => p.pid !== pid), processes.find(p => p.pid === pid)];
      saveReceipt(file, receipt);
    }
    return pid;
  }
  if (command === 'stop') {
    const state = snapshot(Number(args[0] || 0));
    const targets = state.processes.filter(p => owns(receipt, p.pid, state.processes))
      .sort((a, b) => Number(b.pid === receipt.root.pid) - Number(a.pid === receipt.root.pid));
    // Persist verified identities before termination, so an interrupted stop can
    // safely finish even after the launcher and its PPID chain have disappeared.
    receipt.listeners = targets;
    saveReceipt(file, receipt);
    terminateIdentities(targets);
    for (let attempt = 0; attempt < 20; attempt++) {
      if (!snapshot().processes.some(p => targets.some(t => sameProcess(t, p)))) return '';
      await pause(100);
    }
    throw Error('Owned processes did not stop');
  }
  throw Error(`Unknown command: ${command}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { process.stdout.write(String(await run(process.argv.slice(2)))); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
