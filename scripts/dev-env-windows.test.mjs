import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { owns, sameProcess, run } from './dev-env-windows.mjs';

const root = { pid: 10, parent: 1, birth: '100' };
const child = { pid: 20, parent: 10, birth: '200' };
const grandchild = { pid: 30, parent: 20, birth: '300' };
const receipt = { root, listeners: [] };

test('ownership follows native ancestry and rejects recycled or missing parents', () => {
  assert.equal(owns(receipt, 30, [root, child, grandchild]), true);
  assert.equal(owns(receipt, 30, [root, grandchild]), false);
  assert.equal(owns(receipt, 30, [{ ...root, birth: '400' }, child, grandchild]), false);
  assert.equal(owns(receipt, 30, [root, { ...child, birth: '400' }, grandchild]), false);
  assert.equal(owns(receipt, 99, [root, child, grandchild]), false);
  assert.equal(owns(receipt, 30, [root, { ...child, parent: 30 }, grandchild]), false);
});

test('recorded listeners survive parent exit but never PID reuse', () => {
  const recorded = { root, listeners: [grandchild] };
  assert.equal(owns(recorded, 30, [grandchild]), true);
  assert.equal(owns(recorded, 30, [{ ...grandchild, birth: '500' }]), false);
  assert.equal(sameProcess(root, { ...root, birth: '101' }), false);
});

const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(read, description) {
  for (let i = 0; i < 100; i++) {
    const value = await read();
    if (value) return value;
    await delay(100);
  }
  throw Error(`Timed out: ${description}`);
}

test('Git Bash launches, identifies and stops native child listeners without touching foreign processes',
  { skip: process.platform !== 'win32', timeout: 180000 }, async () => {
    const dir = mkdtempSync(join(tmpdir(), 'multica-dev-process-'));
    const file = join(dir, 'api.windows.json');
    const portFile = join(dir, 'port.json');
    const fixture = join(dir, 'fixture.mjs');
    writeFileSync(fixture, `
      import { spawn } from 'node:child_process';
      import { createServer } from 'node:http';
      import { writeFileSync } from 'node:fs';
      if (process.argv[2] === 'child') {
        const server = createServer((req, res) => res.end(JSON.stringify({pid:process.pid})));
        server.listen(0, '127.0.0.1', () => writeFileSync(process.argv[3], JSON.stringify({port:server.address().port,pid:process.pid})));
      } else {
        spawn(process.execPath, [process.argv[1], 'child', process.argv[2]], {stdio:'ignore'});
        setInterval(() => {}, 1000);
      }
    `);
    const foreign = createServer((req, res) => res.end('foreign'));
    foreign.listen(0, '127.0.0.1');
    await once(foreign, 'listening');
    let port;
    try {
      // Exercise the actual Git Bash entry point, including MSYS path conversion
      // and desktop's env-assignment launch prefix. No installed agent CLI runs.
      const bash = spawnSync('bash.exe', ['-lc', `
        source scripts/dev-env.sh
        STATE_DIR="$FIXTURE_DIR"; LOG_DIR="$FIXTURE_DIR"
        launch_detached api env FIXTURE_MARKER=yes node "$FIXTURE_FILE" "$FIXTURE_PORT"
      `], { encoding: 'utf8', env: { ...process.env, FIXTURE_DIR: dir.replaceAll('\\', '/'),
        FIXTURE_FILE: fixture.replaceAll('\\', '/'), FIXTURE_PORT: portFile.replaceAll('\\', '/') } });
      assert.equal(bash.status, 0, bash.stderr);
      const data = await waitFor(() => existsSync(portFile) && JSON.parse(readFileSync(portFile, 'utf8')), 'listener');
      port = data.port;
      assert.equal(await run(['listener', String(port)]), data.pid);
      assert.equal(await run(['owns', file, String(data.pid)]), data.pid);
      await run(['record', file, String(data.pid)]);
      await assert.rejects(run(['owns', file, String(process.pid)]), /not owned/);
      const valid = JSON.parse(readFileSync(file, 'utf8'));
      writeFileSync(file, JSON.stringify({ root: { ...valid.root, birth: '1' }, listeners: [] }));
      await assert.rejects(run(['owns', file, String(data.pid)]), /not owned/);
      await run(['stop', file, String(port)]);
      assert.equal(await run(['listener', String(port)]), data.pid, 'stale receipt must not kill listener');
      writeFileSync(file, JSON.stringify(valid));
      await run(['stop', file, String(port)]);
      assert.equal(await run(['listener', String(port)]), '');
      assert.equal(await run(['listener', String(foreign.address().port)]), process.pid);
      assert.equal(await (await fetch(`http://127.0.0.1:${foreign.address().port}`)).text(), 'foreign');
    } finally {
      try { await run(['stop', file, String(port || 0)]); } finally {
        foreign.closeAllConnections();
        await new Promise(resolve => foreign.close(resolve));
        rmSync(dir, { recursive: true, force: true });
      }
    }
  });
