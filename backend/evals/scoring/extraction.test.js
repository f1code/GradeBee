// Runs from backend/ `make test` (root make test and CI both call it).
const test = require('node:test');
const assert = require('node:assert');
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
