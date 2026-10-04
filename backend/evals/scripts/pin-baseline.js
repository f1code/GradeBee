#!/usr/bin/env node
/**
 * Copies promptfoo result JSONs to baseline paths, keeping only what
 * diff-baseline.js reads. The raw output also carries the eval id, share URL
 * and full config, which change every run and would bury score changes in
 * the PR diff.
 *
 * Checks every result before writing any baseline, so one failed domain
 * leaves all baselines untouched.
 *
 * Usage:
 *   node evals/scripts/pin-baseline.js <result.json> <baseline.json> [<result.json> <baseline.json> ...]
 */
'use strict';

const fs = require('fs');
const path = require('path');

// promptfoo ResultFailureReason.ERROR: the provider call or a transform threw
// (e.g. API 503), so the row scores 0 without the model being graded.
const FAILURE_ERROR = 2;

const args = process.argv.slice(2);

if (args.length === 0 || args.length % 2 !== 0) {
  console.error('Usage: node pin-baseline.js <result.json> <baseline.json> [<result.json> <baseline.json> ...]');
  process.exit(1);
}

const pins = [];
const problems = [];

for (let i = 0; i < args.length; i += 2) {
  const [inputPath, outputPath] = [args[i], args[i + 1]];

  const data = JSON.parse(fs.readFileSync(path.resolve(inputPath), 'utf8'));
  const results = data.results.results;

  const errored = results.filter(r => r.failureReason === FAILURE_ERROR);
  for (const r of errored) {
    const desc = r.description || r.testCase?.description || '(unnamed)';
    const provider = r.provider?.label || r.provider?.id || 'unknown';
    const error = String(r.error || '').split('\n')[0].slice(0, 160);
    problems.push(`${inputPath}: ${desc} [${provider}] errored: ${error}`);
  }
  pins.push({
    inputPath,
    outputPath,
    pinned: { timestamp: data.results.timestamp, results: { results } },
  });
}

if (problems.length > 0) {
  console.error('Not pinning any baseline:');
  for (const p of problems) console.error(`  ${p}`);
  console.error('Re-run the eval once the errors clear.');
  process.exit(1);
}

for (const { inputPath, outputPath, pinned } of pins) {
  fs.mkdirSync(path.dirname(path.resolve(outputPath)), { recursive: true });
  fs.writeFileSync(path.resolve(outputPath), JSON.stringify(pinned, null, 2) + '\n');
  console.log(`Pinned ${pinned.results.results.length} results from ${inputPath} → ${outputPath}`);
}
