import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const require = createRequire(import.meta.url);
const turbo = process.env.MULTICA_TEST_TURBO_BINARY || require.resolve("turbo/bin/turbo");
const config = JSON.parse(readFileSync(join(root, "turbo.json"), "utf8")
  .replace(/^\s*\/\/.*$/gm, ""));
const pinnedVersion = readFileSync(join(root, "pnpm-lock.yaml"), "utf8")
  .match(/\n  turbo@([^:]+):/)[1];
assert.equal(execFileSync(process.execPath, [turbo, "--version"], { encoding: "utf8" }).trim(), pinnedVersion);

for (const explicitCache of [false, true]) {
  test(`Turbo strict mode preserves native Go paths (${explicitCache ? "explicit" : "default"} cache)`,
    { timeout: 180000 }, () => {
      const dir = mkdtempSync(join(tmpdir(), "multica turbo go "));
      try {
        const env = { ...process.env, HOME: join(dir, "home"),
          LOCALAPPDATA: join(dir, "local app data"), APPDATA: join(dir, "roaming app data"),
          USERPROFILE: join(dir, "profile"), GOTMPDIR: join(dir, "go temp"),
          TMPDIR: join(dir, "temp"), TMP: join(dir, "temp"), TEMP: join(dir, "temp"),
          MULTICA_TEST_UNLISTED: "must-not-reach-child", TURBO_TELEMETRY_DISABLED: "1" };
        for (const key of ["GOCACHE", "GOPATH", "GOMODCACHE", "XDG_CACHE_HOME"]) delete env[key];
        if (explicitCache) {
          env.GOCACHE = join(dir, "go cache");
          env.GOPATH = join(dir, "go path");
          env.GOMODCACHE = join(dir, "module cache");
        }
        const keys = ["LOCALAPPDATA", "APPDATA", "USERPROFILE", "GOCACHE", "GOPATH", "GOMODCACHE", "GOTMPDIR", "TMPDIR", "TMP", "TEMP"];
        const expected = Object.fromEntries(keys.filter(k => env[k] !== undefined).map(k => [k, env[k]]));
        for (const path of [env.HOME, ...Object.values(expected)]) mkdirSync(path, { recursive: true });
        writeFileSync(join(dir, "package.json"), JSON.stringify({ name: "turbo-go-fixture", private: true,
          packageManager: "pnpm@10.28.2", scripts: { probe: "node probe.cjs" } }));
        writeFileSync(join(dir, "pnpm-lock.yaml"), "lockfileVersion: '9.0'\nimporters:\n  .: {}\n");
        writeFileSync(join(dir, "turbo.json"), JSON.stringify({
          globalPassThroughEnv: config.globalPassThroughEnv, tasks: { probe: { cache: false } },
        }));
        writeFileSync(join(dir, "expected.json"), JSON.stringify(expected));
        writeFileSync(join(dir, "main.go"), 'package main\nfunc main() { println("fixture-ok") }\n');
        writeFileSync(join(dir, "probe.cjs"), `
          const assert = require('node:assert/strict');
          const {execFileSync} = require('node:child_process');
          const {readFileSync, writeFileSync} = require('node:fs');
          const {join} = require('node:path');
          const expected = JSON.parse(readFileSync('expected.json', 'utf8'));
          for (const [key, value] of Object.entries(expected)) assert.equal(process.env[key], value, key);
          assert.equal(process.env.MULTICA_TEST_UNLISTED, undefined, 'strict filtering must remain active');
          const cache = execFileSync('go', ['env', 'GOCACHE'], {encoding:'utf8'}).trim();
          assert.notEqual(cache, 'off');
          assert.ok(cache.length > 0);
          if (process.platform === 'win32') assert.equal(cache, expected.GOCACHE || join(expected.LOCALAPPDATA, 'go-build'));
          const binary = join(process.cwd(), process.platform === 'win32' ? 'fixture.exe' : 'fixture');
          execFileSync('go', ['build', '-o', binary, 'main.go'], {stdio:'pipe'});
          writeFileSync('result.json', JSON.stringify({cache, built:true}));
        `);
        execFileSync(process.execPath, [turbo, "run", "probe", "--env-mode=strict"],
          { cwd: dir, env, encoding: "utf8", timeout: 150000, stdio: "pipe" });
        assert.equal(JSON.parse(readFileSync(join(dir, "result.json"), "utf8")).built, true);
      } finally { rmSync(dir, { recursive: true, force: true }); }
    });
}
