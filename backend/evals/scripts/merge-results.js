#!/usr/bin/env node
/**
 * merge-results.js
 *
 * Merges multiple promptfoo eval result JSONs into a single combined result.
 * The merged output has the same structure as a single promptfoo run, with all
 * test results concatenated in the results.results array.
 *
 * Usage:
 *   node evals/scripts/merge-results.js <output.json> <input1.json> [input2.json ...]
 *
 * Example:
 *   node evals/scripts/merge-results.js evals/results/merged.json \
 *     evals/results/extract.json evals/results/report.json
 */
'use strict';

const fs = require('fs');
const path = require('path');

const [, , outputPath, ...inputPaths] = process.argv;

if (!outputPath || inputPaths.length === 0) {
  console.error('Usage: node merge-results.js <output.json> <input1.json> [input2.json ...]');
  process.exit(1);
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

// Load all input files and collect their results arrays.
const allResults = [];
for (const inputPath of inputPaths) {
  const data = loadResults(path.resolve(inputPath));
  if (!data) {
    console.error(`Skipping ${inputPath} — could not load.`);
    continue;
  }
  // promptfoo result JSON structure: { results: { results: Array<{...}> } }
  const results = (data.results && data.results.results) ? data.results.results : [];
  allResults.push(...results);
}

if (allResults.length === 0) {
  console.error('No results to merge.');
  process.exit(1);
}

// Build merged output with the same top-level structure as promptfoo.
const merged = {
  version: 3,
  timestamp: new Date().toISOString(),
  config: {
    description: 'GradeBee LLM evaluation harness — merged extract + report',
  },
  results: {
    version: 3,
    timestamp: new Date().toISOString(),
    results: allResults,
    stats: {
      success: allResults.filter(r => r.success).length,
      failure: allResults.filter(r => !r.success).length,
      tokenUsage: { total: 0, cached: 0, completion: 0, prompt: 0 },
    },
    table: {
      head: {
        vars: [],
      },
      body: [],
    },
  },
};

// Aggregate token usage from all results.
for (const r of allResults) {
  if (r.tokenUsage) {
    merged.results.stats.tokenUsage.total += r.tokenUsage.total || 0;
    merged.results.stats.tokenUsage.cached += r.tokenUsage.cached || 0;
    merged.results.stats.tokenUsage.completion += r.tokenUsage.completion || 0;
    merged.results.stats.tokenUsage.prompt += r.tokenUsage.prompt || 0;
  }
}

// Write merged output.
const outDir = path.dirname(outputPath);
if (!fs.existsSync(outDir)) {
  fs.mkdirSync(outDir, { recursive: true });
}
fs.writeFileSync(path.resolve(outputPath), JSON.stringify(merged, null, 2) + '\n');
console.log(`Merged ${allResults.length} results → ${outputPath}`);
