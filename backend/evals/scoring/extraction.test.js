// Runs from backend/ `make test` (root make test and CI both call it).
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const score = require('./extraction.js');

// Three expected children plus a phantom: precision 3/4 passes on its own,
// so only no_note_students can fail this row.
const expected = { expected_students: [{ name: 'Nadia' }, { name: 'Bruno' }, { name: 'Lina' }] };
const note = (name) => ({ name, quoted_text: 'worked well' });
const run = (students, extra = {}) =>
  score(JSON.stringify({ students }), { config: { expected: { ...expected, ...extra } } });

test('a listed child with a note fails the row and is named', async () => {
  const r = await run([note('Nadia'), note('Bruno'), note('Lina'), note(' théa ')], { no_note_students: ['Théa'] });
  assert.strictEqual(r.pass, false);
  assert.match(r.reason, /Théa must get no note/);
});

test('a listed child without a note passes', async () => {
  const r = await run([note('Nadia'), note('Bruno'), note('Lina')], { no_note_students: ['Théa'] });
  assert.strictEqual(r.pass, true);
});

test('the field moves no score', async () => {
  const students = [note('Nadia'), note('Bruno'), note('Lina'), note('Théa')];
  const without = await run(students);
  const withField = await run(students, { no_note_students: ['Théa'] });
  assert.strictEqual(without.pass, true);
  assert.strictEqual(withField.score, without.score);
  assert.deepStrictEqual(withField.namedScores, without.namedScores);
});

test('kept text missing from every passage fails the row', async () => {
  const kept = 'Nadia worked well';
  const ok = await score(JSON.stringify({ students: [], kept }), { config: { expected: { must_keep_substrings: ['worked well'] } } });
  const lost = await score(JSON.stringify({ students: [], kept: '' }), { config: { expected: { must_keep_substrings: ['worked well'] } } });
  assert.strictEqual(ok.pass, true);
  assert.strictEqual(lost.pass, false);
  assert.match(lost.reason, /"worked well" is in no kept passage/);
});

// `hard` is what scripts/diff-baseline.js counts regressions on.
const fuzzyShaped = {
  expected_students: [{ name: 'Théo', must_quote_substrings: ['full sentences'], should_quote_substrings: ['/Yes,\\s*I can/'] }],
};
const theo = (quoted_text, extra = {}) =>
  score(JSON.stringify({ students: [{ name: 'Théo', quoted_text }] }), { config: { expected: { ...fuzzyShaped, ...extra } } });

test('a missed preferred phrase lowers score, not hard or pass', async () => {
  const r = await theo('making full sentences');
  assert.strictEqual(r.pass, true);
  assert.strictEqual(r.namedScores.hard, 1);
  assert.strictEqual(r.score, 0.8);
});

test('a reached preferred phrase scores 1', async () => {
  const r = await theo('making full sentences. Yes,\n\nI can.');
  assert.strictEqual(r.score, 1);
  assert.strictEqual(r.namedScores.hard, 1);
});

// Without the wantedTotal guard the 0/0 preference axis makes score NaN.
test('a fixture with no preferred phrases scores 1', async () => {
  const r = await run([note('Nadia'), note('Bruno'), note('Lina')]);
  assert.strictEqual(r.pass, true);
  assert.strictEqual(r.score, 1);
  assert.strictEqual(r.namedScores.hard, 1);
});

// Only must_not_extract fails this row: names, quotes and attribution are clean.
test('a must_not_extract leak fails the row and lowers hard', async () => {
  const text = 'making full sentences. The principal stopped by.';
  const r = await theo(text);
  const leaked = await theo(text, { must_not_extract: ['principal'] });
  assert.strictEqual(r.pass, true);
  assert.strictEqual(leaked.pass, false);
  assert.strictEqual(leaked.namedScores.hard, 0.875);
});

// Golden replay: stored model outputs must keep their verdict. A scorer change
// that re-grades them on purpose: UPDATE_GOLDENS=1, then review the diff.
const goldenDir = path.join(__dirname, 'goldens');

for (const file of fs.readdirSync(goldenDir).filter((f) => f.endsWith('.json'))) {
  test(`golden ${file}`, async () => {
    const goldenPath = path.join(goldenDir, file);
    const g = JSON.parse(fs.readFileSync(goldenPath, 'utf8'));
    const r = await score(JSON.stringify(g.output), { config: { expected: g.expected } });
    const got = { pass: r.pass, score: r.score, hard: r.namedScores.hard };
    if (process.env.UPDATE_GOLDENS) {
      fs.writeFileSync(goldenPath, JSON.stringify({ ...g, ...got }, null, 2) + '\n');
      return;
    }
    assert.deepStrictEqual(got, { pass: g.pass, score: g.score, hard: g.hard }, r.reason);
  });
}
