#!/usr/bin/env node
// Stability gate `test` slot: run every Go test in both modules (agent,
// server) and exit 0 ONLY if tests ran, none failed and NONE WERE SKIPPED.
//
// WHY SKIPS ARE RED. The server's API tests (auth, RBAC, kill tokens,
// rollouts) call t.Skip when Postgres or Redis is unreachable, and the agent's
// capture tests skip without a desktop session. `go test` exits 0 over a run
// in which those tests never executed, so its exit code alone would report a
// green suite that examined nothing that matters. This runner reads the
// `go test -json` event stream and treats any skipped test as a failure.
// Unknown is never clean.
//
// A package with no test files is reported (go emits a package-level "skip")
// but is not a test skip: there was nothing to run.
//
// Time bound: -timeout per test binary, so a hung test (a deadlock, a probe
// that never returns) fails its package instead of holding the gate.
//
// Usage: node scripts/stability/go-test.mjs   (run from the repo root)

import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const MODULES = ['agent', 'server'];
const PER_BINARY_TIMEOUT = '5m';
const MAX_LISTED = 25;

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');

function runModule(moduleDir) {
  return new Promise((resolve) => {
    const result = {
      module: moduleDir,
      passed: 0,
      failed: [],
      skipped: [],
      failedPackages: [],
      noTestPackages: 0,
      stderr: '',
      exitCode: null,
      spawnError: null,
    };
    const child = spawn('go', ['test', '-json', `-timeout=${PER_BINARY_TIMEOUT}`, './...'], {
      cwd: path.join(repoRoot, moduleDir),
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true,
    });
    child.on('error', (err) => {
      result.spawnError = err.message;
    });
    child.stderr.on('data', (chunk) => {
      result.stderr += chunk.toString();
    });
    const lines = createInterface({ input: child.stdout });
    lines.on('line', (line) => {
      let event;
      try {
        event = JSON.parse(line);
      } catch {
        // Non-JSON stdout is a build or link message; keep it as evidence.
        result.stderr += `${line}\n`;
        return;
      }
      const pkg = String(event.Package ?? '').replace(/^github\.com\/sentinel\//, '');
      if (event.Test) {
        if (event.Action === 'pass') result.passed += 1;
        else if (event.Action === 'fail') result.failed.push(`${pkg} ${event.Test}`);
        else if (event.Action === 'skip') result.skipped.push(`${pkg} ${event.Test}`);
      } else if (event.Action === 'fail') {
        result.failedPackages.push(pkg);
      } else if (event.Action === 'skip') {
        result.noTestPackages += 1;
      }
    });
    child.on('close', (code) => {
      result.exitCode = code;
      resolve(result);
    });
  });
}

function list(label, items) {
  if (items.length === 0) return;
  console.error(`  ${label} (${items.length}):`);
  for (const item of items.slice(0, MAX_LISTED)) console.error(`    ${item}`);
  if (items.length > MAX_LISTED) console.error(`    ... and ${items.length - MAX_LISTED} more`);
}

const results = [];
for (const moduleDir of MODULES) {
  const started = Date.now();
  const r = await runModule(moduleDir);
  r.seconds = Math.round((Date.now() - started) / 1000);
  results.push(r);
}

let red = false;
for (const r of results) {
  const problems = [];
  if (r.spawnError) problems.push(`could not start go: ${r.spawnError}`);
  if (r.failed.length) problems.push(`${r.failed.length} failed test(s)`);
  if (r.failedPackages.length) problems.push(`${r.failedPackages.length} failed package(s)`);
  if (r.skipped.length) problems.push(`${r.skipped.length} skipped test(s)`);
  if (r.passed === 0) problems.push('no test passed (nothing was examined)');
  if (r.exitCode !== 0 && problems.length === 0) problems.push(`go test exited ${r.exitCode}`);

  const summary = `${r.module}: ${r.passed} passed, ${r.failed.length} failed, ${r.skipped.length} skipped, ` +
    `${r.noTestPackages} package(s) without tests, ${r.seconds}s`;
  if (problems.length === 0) {
    console.log(`ok   ${summary}`);
    continue;
  }
  red = true;
  console.error(`FAIL ${summary}: ${problems.join('; ')}`);
  list('failed packages', r.failedPackages);
  list('failed tests', r.failed);
  list('skipped tests (a skipped test is not a passing test)', r.skipped);
  const tail = r.stderr.trim().split('\n').slice(-15).join('\n');
  if (tail) console.error(`  go stderr (tail):\n${tail.replace(/^/gm, '    ')}`);
}

process.exit(red ? 1 : 0);
