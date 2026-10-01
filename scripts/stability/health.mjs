#!/usr/bin/env node
// Stability gate `health` slot: build the real entry points and prove they
// start.
//
//   agent   builds cmd/sentinel-agent and runs `-version`. It must exit 0 and
//           print "Sentinel Agent v<semver>" and the host OS. That proves the
//           binary links, its package init (Windows service, crypto and
//           capture packages) runs, and flag parsing reaches main.
//
//   server  builds cmd/sentinel and starts it with an EMPTY environment in an
//           empty directory. The server cannot run without Postgres and Redis,
//           so the probe checks the startup path that runs on every boot:
//           config load, then Validate(), which must refuse to start and
//           name the missing DATABASE_URL. A server that started anyway, or
//           crashed before validating, fails this check.
//
// Both binaries are built into a temporary directory that is always removed.
// Every step is time-bounded, and any unexpected result is a non-zero exit.
//
// Usage: node scripts/stability/health.mjs   (run from the repo root)

import { spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const exe = process.platform === 'win32' ? '.exe' : '';
const BUILD_TIMEOUT_MS = 10 * 60 * 1000;
const RUN_TIMEOUT_MS = 30 * 1000;

class HealthFailure extends Error {}

function build(moduleDir, pkg, outFile) {
  const r = spawnSync('go', ['build', '-o', outFile, pkg], {
    cwd: path.join(repoRoot, moduleDir),
    encoding: 'utf8',
    timeout: BUILD_TIMEOUT_MS,
    windowsHide: true,
  });
  if (r.error) throw new HealthFailure(`go build ${moduleDir}/${pkg}: ${r.error.message}`);
  if (r.status !== 0) throw new HealthFailure(`go build ${moduleDir}/${pkg} exited ${r.status}:\n${r.stderr}`);
}

function run(file, args, options) {
  const r = spawnSync(file, args, { encoding: 'utf8', timeout: RUN_TIMEOUT_MS, windowsHide: true, ...options });
  if (r.error) throw new HealthFailure(`${path.basename(file)} ${args.join(' ')}: ${r.error.message}`);
  return { status: r.status, out: `${r.stdout ?? ''}${r.stderr ?? ''}` };
}

function probeAgent(dir) {
  const bin = path.join(dir, `sentinel-agent${exe}`);
  build('agent', './cmd/sentinel-agent', bin);
  const { status, out } = run(bin, ['-version'], { cwd: dir });
  if (status !== 0) throw new HealthFailure(`sentinel-agent -version exited ${status}:\n${out}`);
  const version = out.match(/^Sentinel Agent v(\d+\.\d+\.\d+\S*)\s*$/m);
  if (!version) throw new HealthFailure(`sentinel-agent -version did not print a version line:\n${out}`);
  if (!/^OS: \w+\s*$/m.test(out)) throw new HealthFailure(`sentinel-agent -version did not report its OS:\n${out}`);
  console.log(`ok   agent: sentinel-agent -version -> v${version[1]}`);
}

function probeServer(dir) {
  const bin = path.join(dir, `sentinel${exe}`);
  build('server', './cmd/sentinel', bin);
  // Only what the OS loader needs. No DATABASE_URL, REDIS_URL or secrets can
  // leak in from the host, and an empty cwd means no stray config file is read.
  const env = {};
  for (const key of ['SystemRoot', 'SYSTEMROOT', 'windir', 'TEMP', 'TMP']) {
    if (process.env[key]) env[key] = process.env[key];
  }
  const { status, out } = run(bin, [], { cwd: dir, env });
  if (status === 0 || status === null) {
    throw new HealthFailure(`sentinel server with no configuration exited ${status}; it must refuse to start:\n${out}`);
  }
  if (!out.includes('Configuration validation failed') || !out.includes('DATABASE_URL is required')) {
    throw new HealthFailure(`sentinel server did not reach config validation (expected it to reject the missing DATABASE_URL):\n${out}`);
  }
  console.log('ok   server: sentinel starts, loads config, and refuses to run without DATABASE_URL');
}

const workDir = mkdtempSync(path.join(tmpdir(), 'sentinel-health-'));
let code = 0;
try {
  probeAgent(workDir);
  probeServer(workDir);
  console.log('OK: health probes passed');
} catch (err) {
  code = 1;
  console.error(`FAIL: ${err instanceof HealthFailure ? err.message : err?.stack ?? err}`);
} finally {
  rmSync(workDir, { recursive: true, force: true, maxRetries: 3 });
}
process.exit(code);
