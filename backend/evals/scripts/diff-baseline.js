#!/usr/bin/env node
/**
 * diff-baseline.js
 *
 * Compares the latest promptfoo eval results JSON against a pinned baseline
 * and prints a per-case, per-metric diff table.
 *
 * Usage:
 *   node evals/scripts/diff-baseline.js evals/baseline.json evals/results/20260520-120000.json
 *
 * Exit code is always 0 — human-in-the-loop interpretation.
 */
'use strict';

const fs = require('fs');
const path = require('path');

const [, , baselinePath, currentPath] = process.argv;

if (!baselinePath || !currentPath) {
  console.error('Usage: node diff-baseline.js <baseline.json> <current.json>');
  process.exit(0);
}

function loadResults(filePath) {
  try {
    const raw = fs.readFileSync(filePath, 'utf8');
    return JSON.parse(raw);
  } catch (e) {
    console.error(`Could not read ${filePath}: ${e.message}`);
    return null;
  }
}

const baseline = loadResults(path.resolve(baselinePath));
const current  = loadResults(path.resolve(currentPath));

if (!baseline || !current) {
  console.log('Skipping diff — could not load one or both result files.');
  process.exit(0);
}

// promptfoo result JSON structure: { results: { results: Array<{description, score, pass, ...}> } }
const baselineResults = (baseline.results && baseline.results.results) ? baseline.results.results : [];
const currentResults  = (current.results  && current.results.results)  ? current.results.results  : [];

// Index by description for lookup
function indexByDesc(arr) {
  const map = {};
  for (const r of arr) {
    map[r.description || r.testCase?.description || '(unnamed)'] = r;
  }
  return map;
}

const baseIdx = indexByDesc(baselineResults);
const currIdx = indexByDesc(currentResults);

const allDescs = new Set([...Object.keys(baseIdx), ...Object.keys(currIdx)]);

const GREEN  = '\x1b[32m';
const RED    = '\x1b[31m';
const YELLOW = '\x1b[33m';
const RESET  = '\x1b[0m';
const BOLD   = '\x1b[1m';

function colour(delta) {
  if (delta > 0.01)  return GREEN;
  if (delta < -0.01) return RED;
  return YELLOW;
}

console.log(`\n${BOLD}=== Eval baseline diff ===${RESET}`);
console.log(`Baseline : ${baselinePath}`);
console.log(`Current  : ${currentPath}`);
console.log('');

const header = ['Case', 'Baseline score', 'Current score', 'Delta', 'Pass baseline', 'Pass current'];
const rows   = [header];

let regressions = 0;
let improvements = 0;

for (const desc of allDescs) {
  const b = baseIdx[desc];
  const c = currIdx[desc];
  const bScore = b ? (b.score ?? b.namedScores?.score ?? 0) : null;
  const cScore = c ? (c.score ?? c.namedScores?.score ?? 0) : null;
  const bPass  = b ? (b.gradingResult != null ? b.gradingResult.pass : b.pass) : null;
  const cPass  = c ? (c.gradingResult != null ? c.gradingResult.pass : c.pass) : null;

  const delta   = (bScore !== null && cScore !== null) ? (cScore - bScore) : null;
  const deltaStr = delta !== null
    ? `${colour(delta)}${delta > 0 ? '+' : ''}${delta.toFixed(3)}${RESET}`
    : 'n/a';

  if (delta !== null && delta < -0.05) regressions++;
  if (delta !== null && delta >  0.05) improvements++;

  rows.push([
    desc.length > 60 ? desc.slice(0, 57) + '…' : desc,
    bScore !== null ? bScore.toFixed(3) : '—',
    cScore !== null ? cScore.toFixed(3) : '—',
    deltaStr,
    bPass !== null ? (bPass ? `${GREEN}PASS${RESET}` : `${RED}FAIL${RESET}`) : '—',
    cPass !== null ? (cPass ? `${GREEN}PASS${RESET}` : `${RED}FAIL${RESET}`) : '—',
  ]);
}

// Print table
const colWidths = header.map((_, i) => Math.max(...rows.map(r => stripAnsi(String(r[i])).length)));

function stripAnsi(s) { return s.replace(/\x1b\[[0-9;]*m/g, ''); }
function pad(s, w)    { const plain = stripAnsi(s); return s + ' '.repeat(Math.max(0, w - plain.length)); }

const separator = colWidths.map(w => '-'.repeat(w + 2)).join('+');
console.log(separator);
for (let i = 0; i < rows.length; i++) {
  const row = rows[i];
  console.log('| ' + row.map((cell, j) => pad(String(cell), colWidths[j])).join(' | ') + ' |');
  if (i === 0) console.log(separator);
}
console.log(separator);
console.log('');
console.log(`Regressions (delta < -0.05): ${regressions}`);
console.log(`Improvements (delta > +0.05): ${improvements}`);
console.log('');
console.log('To update the baseline:  make eval-baseline');
console.log('');
// Always exit 0 — human interpretation
process.exit(0);
