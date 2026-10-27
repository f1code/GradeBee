// Runs from backend/ `make test`.
const test = require('node:test');
const assert = require('node:assert');
const { descPattern, tally } = require('./confirm-regressions.js');

test('descPattern matches each description whole, punctuation literal', () => {
  const re = new RegExp(descPattern(['report: Sam (text), 5 notes?', 'a.b']));
  assert.ok(re.test('report: Sam (text), 5 notes?'));
  assert.ok(re.test('a.b'));
  assert.ok(!re.test('axb'));
  assert.ok(!re.test('report: Sam (text), 5 notes? and more'));
});

test('tally counts reruns that regress against the baseline row', () => {
  const row = (description, score, pass, extra = {}) =>
    ({ description, provider: { label: 'gradebee-report' }, score, pass, ...extra });
  const base = new Map([['a', row('a', 0.9, true)], ['b', row('b', 0.9, true)]]);
  const reruns = [
    row('a', 0.9, false), row('a', 0.6, true), row('a', 0.9, true), // flip, drop, fine
    row('b', 0.85, true), row('b', 0, false, { failureReason: 2 }), row('b', 0.9, true),
    { ...row('a', 0, false), provider: { label: 'gold-reference' } },
  ];
  const t = tally(base, reruns, 0.15);
  assert.deepStrictEqual(t.get('a'), { regressed: 2, errored: 0 });
  assert.deepStrictEqual(t.get('b'), { regressed: 0, errored: 1 });
});
