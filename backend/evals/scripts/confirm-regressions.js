#!/usr/bin/env node
/**
 * Reruns each canonical report case that regressed against the baseline
 * RUNS times, fresh generation and judging, and confirms it only when at
 * least CONFIRM_AT reruns regress too. One run is noisy: the report model
 * writes a different report each time.
 *
 * Usage:
 *   node evals/scripts/confirm-regressions.js [--band=N] <baseline.json> <result.json> <confirm-output.json>
 */
'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const CANONICAL = 'gradebee-report';
const RUNS = 3;
const CONFIRM_AT = 2;
// promptfoo ResultFailureReason.ERROR (see pin-baseline.js).
const FAILURE_ERROR = 2;

const desc = r => r.description || r.testCase?.description || '(unnamed)';
const provider = r => r.provider?.label || r.provider?.id || 'unknown';
const score = r => r.score ?? r.namedScores?.score ?? 0;
const hard = r => r.namedScores?.hard ?? score(r);
const pass = r => (r.gradingResult != null ? r.gradingResult.pass : r.pass);

// Shared with diff-baseline.js. Epsilon: float means land a hair either side
// of a band edge. A pass flip counts on its own, since a floor breach can move
// the mean less than the band.
function regressed(b, c, band) {
  return hard(c) - hard(b) < -band - 1e-9 || (pass(b) && !pass(c));
}

// Descriptions carry punctuation; anchored so a description sharing another's
// prefix stays out.
function descPattern(descs) {
  return '^(' + descs.map(d => d.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|') + ')$';
}

// Map of description -> { regressed, errored } over the rerun rows.
function tally(baseByDesc, reruns, band) {
  const out = new Map();
  for (const r of reruns) {
    if (provider(r) !== CANONICAL || !baseByDesc.has(desc(r))) continue;
    const t = out.get(desc(r)) || { regressed: 0, errored: 0 };
    if (r.failureReason === FAILURE_ERROR) t.errored++;
    else if (regressed(baseByDesc.get(desc(r)), r, band)) t.regressed++;
    out.set(desc(r), t);
  }
  return out;
}

const rows = file => JSON.parse(fs.readFileSync(file, 'utf8')).results.results;

function canonicalByDesc(results) {
  const m = new Map();
  for (const r of results) if (provider(r) === CANONICAL && !m.has(desc(r))) m.set(desc(r), r);
  return m;
}

function main() {
  const args = process.argv.slice(2);
  const bandFlag = args.find(a => a.startsWith('--band='));
  const band = bandFlag ? Number(bandFlag.slice('--band='.length)) : 0.05;
  const [baselinePath, resultPath, outPath] = args.filter(a => !a.startsWith('--'));
  if (!outPath) {
    console.error('Usage: node confirm-regressions.js [--band=N] <baseline.json> <result.json> <confirm-output.json>');
    process.exit(1);
  }
  // diff-baseline.js already said why when either file is missing.
  if (!fs.existsSync(baselinePath) || !fs.existsSync(resultPath)) return;

  const base = canonicalByDesc(rows(baselinePath));
  const curr = canonicalByDesc(rows(resultPath));
  const flagged = [...curr].filter(([d, c]) => base.has(d) && regressed(base.get(d), c, band)).map(([d]) => d);
  if (flagged.length === 0) return;

  console.log(`\n=== Confirming ${flagged.length} report regression(s): ${RUNS} fresh reruns each ===`);
  const evalsDir = path.resolve(__dirname, '..');
  const out = path.resolve(outPath);
  fs.rmSync(out, { force: true });
  // Cache off, or reruns replay the flagged output. Exit code ignored: promptfoo
  // fails on any failed assertion.
  spawnSync('pnpm', ['exec', 'promptfoo', 'eval',
    '--config', 'promptfooconfig.report.yaml',
    '--filter-pattern', descPattern(flagged),
    '--filter-providers', `^${CANONICAL}$`,
    '--repeat', String(RUNS), '--no-cache', '--no-table',
    '--output', out,
  ], { cwd: evalsDir, stdio: 'inherit', env: { ...process.env, PROMPTFOO_CACHE_ENABLED: 'false' } });
  if (!fs.existsSync(out)) {
    console.log('Confirm run wrote no output: it failed, see above.');
    return;
  }

  const t = tally(base, rows(out), band);
  let confirmed = 0;
  for (const d of flagged) {
    const { regressed: n = 0, errored = 0 } = t.get(d) || {};
    const ok = n >= CONFIRM_AT;
    if (ok) confirmed++;
    console.log(`${ok ? 'CONFIRMED' : 'cleared  '} ${n}/${RUNS} regressed${errored ? `, ${errored} errored` : ''}  ${d}`);
  }
  console.log(`Confirmed report regressions: ${confirmed} of ${flagged.length} flagged (${out})`);
}

if (require.main === module) main();

module.exports = { regressed, descPattern, tally };
