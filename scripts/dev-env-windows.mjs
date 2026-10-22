// Native Windows PIDs must not be compared with Git Bash's emulated PIDs.
import { spawn, execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, openSync, closeSync, existsSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

function powershell(source) {
  return execFileSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-EncodedCommand',
    Buffer.from(`$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; ${source}`, 'utf16le').toString('base64')],
  { encoding: 'utf8', timeout: 30000, windowsHide: true }).trim();
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
    const fd = openSync(log, 'w');
    const child = spawn(input[0], input.slice(1), { env, detached: true, windowsHide: true, stdio: ['ignore', fd, fd] });
    try {
      await new Promise((resolve, reject) => { child.once('spawn', resolve); child.once('error', reject); });
      const root = snapshot().processes.find(p => p.pid === child.pid);
      if (!root) throw Error('Launcher exited before identity was recorded');
      saveReceipt(file, { root, listeners: [] });
      child.unref();
      return root.pid;
    } catch (error) {
      child.kill();
      throw error;
    } finally { closeSync(fd); }
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
    if (targets.length) {
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
