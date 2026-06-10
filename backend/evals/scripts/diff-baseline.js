#!/usr/bin/env node
/**
 * diff-baseline.js
 *
 * Compares the latest promptfoo eval results JSON against a pinned baseline
 * and prints a per-case, per-provider diff table.
 *
 * Canonical providers (gradebee-extract, gradebee-report) are starred ★ and
 * sorted first within each test group — they are the ones tracked in baseline.json.
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

// Canonical providers whose results are stored in baseline.json and used for
// regression tracking. All other providers are treated as comparison/experimental.
const CANONICAL_PROVIDERS = new Set(['gradebee-extract', 'gradebee-report']);

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

// promptfoo result JSON structure: { results: { results: Array<{...}> } }
const baselineResults = (baseline.results && baseline.results.results) ? baseline.results.results : [];
const currentResults  = (current.results  && current.results.results)  ? current.results.results  : [];

// Index by (description, provider) to correctly handle multi-provider test runs.
function indexByKey(arr) {
  const map = {};
  for (const r of arr) {
    const desc     = r.description || r.testCase?.description || '(unnamed)';
    const provider = r.provider?.label || r.provider?.id || 'unknown';
    const key      = `${desc}|||${provider}`;
    if (!map[key]) map[key] = r; // first occurrence wins (promptfoo may repeat)
  }
  return map;
}

const baseIdx = indexByKey(baselineResults);
const currIdx = indexByKey(currentResults);

// Collect all unique (desc, provider) keys across both runs.
const allKeys = new Set([...Object.keys(baseIdx), ...Object.keys(currIdx)]);

// Group by description.
const byDesc = new Map();
for (const key of allKeys) {
  const sep      = key.indexOf('|||');
  const desc     = key.slice(0, sep);
  const provider = key.slice(sep + 3);
  if (!byDesc.has(desc)) byDesc.set(desc, new Set());
  byDesc.get(desc).add(provider);
}

const GREEN  = '\x1b[32m';
const RED    = '\x1b[31m';
const YELLOW = '\x1b[33m';
const CYAN   = '\x1b[36m';
const RESET  = '\x1b[0m';
const BOLD   = '\x1b[1m';
const DIM    = '\x1b[2m';

function colourDelta(delta) {
  if (delta > 0.01)  return GREEN;
  if (delta < -0.01) return RED;
  return YELLOW;
}

function stripAnsi(s) { return s.replace(/\x1b\[[0-9;]*m/g, ''); }
function pad(s, w)    { const plain = stripAnsi(s); return s + ' '.repeat(Math.max(0, w - plain.length)); }

console.log(`\n${BOLD}=== Eval baseline diff ===${RESET}`);
console.log(`Baseline : ${baselinePath}`);
console.log(`Current  : ${currentPath}`);
console.log(`Canonical: ${[...CANONICAL_PROVIDERS].join(', ')} (★)`);
console.log('');

const header = ['Case', 'Provider', 'Baseline', 'Current', 'Delta', 'Pass B', 'Pass C'];
const rows   = [header];

let regressions  = 0;
let improvements = 0;

// Sort descriptions; within each group sort canonical providers first.
const sortedDescs = [...byDesc.keys()].sort();

for (const desc of sortedDescs) {
  const providers = byDesc.get(desc);

  // Sort: canonical first, then alphabetical.
  const sortedProviders = [...providers].sort((a, b) => {
    const aC = CANONICAL_PROVIDERS.has(a) ? 0 : 1;
    const bC = CANONICAL_PROVIDERS.has(b) ? 0 : 1;
    return aC - bC || a.localeCompare(b);
  });

  const shortDesc = desc.length > 52 ? desc.slice(0, 49) + '…' : desc;
  let firstRow = true;

  for (const provider of sortedProviders) {
    const key    = `${desc}|||${provider}`;
    const b      = baseIdx[key];
    const c      = currIdx[key];
    const bScore = b ? (b.score ?? b.namedScores?.score ?? 0) : null;
    const cScore = c ? (c.score ?? c.namedScores?.score ?? 0) : null;
    const bPass  = b ? (b.gradingResult != null ? b.gradingResult.pass : b.pass) : null;
    const cPass  = c ? (c.gradingResult != null ? c.gradingResult.pass : c.pass) : null;

    const delta    = (bScore !== null && cScore !== null) ? (cScore - bScore) : null;
    const deltaStr = delta !== null
      ? `${colourDelta(delta)}${delta > 0 ? '+' : ''}${delta.toFixed(3)}${RESET}`
      : `${DIM}n/a${RESET}`;

    const isCanonical = CANONICAL_PROVIDERS.has(provider);
    if (isCanonical && delta !== null && delta < -0.05) regressions++;
    if (isCanonical && delta !== null && delta >  0.05) improvements++;

    const providerLabel = isCanonical
      ? `${CYAN}${BOLD}★ ${provider}${RESET}`
      : `  ${DIM}${provider}${RESET}`;

    rows.push([
      firstRow ? shortDesc : '',           // only show desc on first row of group
      providerLabel,
      bScore !== null ? bScore.toFixed(3) : `${DIM}—${RESET}`,
      cScore !== null ? cScore.toFixed(3) : `${DIM}—${RESET}`,
      deltaStr,
      bPass !== null ? (bPass ? `${GREEN}PASS${RESET}` : `${RED}FAIL${RESET}`) : `${DIM}—${RESET}`,
      cPass !== null ? (cPass ? `${GREEN}PASS${RESET}` : `${RED}FAIL${RESET}`) : `${DIM}—${RESET}`,
    ]);
    firstRow = false;
  }
}

// Print table.
const colWidths = header.map((_, i) => Math.max(...rows.map(r => stripAnsi(String(r[i])).length)));

const separator = colWidths.map(w => '-'.repeat(w + 2)).join('+');
console.log(separator);
for (let i = 0; i < rows.length; i++) {
  const row = rows[i];
  console.log('| ' + row.map((cell, j) => pad(String(cell), colWidths[j])).join(' | ') + ' |');
  if (i === 0) console.log(separator);
}
console.log(separator);
console.log('');
console.log(`Canonical regressions  (delta < -0.05): ${regressions}`);
console.log(`Canonical improvements (delta > +0.05): ${improvements}`);
console.log('');
console.log('To update the baseline:  make eval-baseline');
console.log('');
// Always exit 0 — human interpretation
process.exit(0);
