// Runs from backend/ `make test`.
const test = require('node:test');
const assert = require('node:assert');
const { chainRounds } = require('./regen-chain.js');

// fakeInner answers each call with the next canned response and records what
// it was asked.
function fakeInner(responses) {
  const calls = [];
  return {
    calls,
    async callApi(prompt, context) {
      calls.push({ prompt, vars: context.vars });
      return responses[calls.length - 1];
    },
  };
}

const builds = [];
async function buildPrompt(vars) {
  builds.push(vars);
  return `prompt for round ${vars.round}`;
}

test('a case without feedback is one plain call', async () => {
  const inner = fakeInner([{ output: 'fresh', cost: 1 }]);
  const res = await chainRounds({ inner, prompt: 'p0', context: { vars: {} }, buildPrompt });
  assert.deepStrictEqual(res, { output: 'fresh', cost: 1 });
  assert.strictEqual(inner.calls.length, 1);
});

test('each later round revises the previous output; cost and tokens sum', async () => {
  builds.length = 0;
  const inner = fakeInner([
    { output: 'round 1', cost: 1, tokenUsage: { total: 10, prompt: 6 } },
    { output: 'round 2', cost: 2, tokenUsage: { total: 20, prompt: 12 } },
    { output: 'round 3', cost: 4, tokenUsage: { total: 40, prompt: 24 }, metadata: { m: 1 } },
  ]);
  const vars = { feedback: '["a","b","c"]', draft: '<p>draft</p>' };
  const res = await chainRounds({ inner, prompt: 'p0', context: { vars }, buildPrompt });

  assert.deepStrictEqual(builds.map((v) => [v.round, v.draft]), [[1, 'round 1'], [2, 'round 2']]);
  assert.deepStrictEqual(inner.calls.map((c) => c.prompt), ['p0', 'prompt for round 1', 'prompt for round 2']);
  assert.strictEqual(res.output, 'round 3');
  assert.strictEqual(res.cost, 7);
  assert.deepStrictEqual(res.tokenUsage, { total: 70, prompt: 42 });
  assert.deepStrictEqual(res.metadata, { m: 1, earlierRounds: ['round 1', 'round 2'] });
});

test('an error ends the chain and comes back as is', async () => {
  builds.length = 0;
  const failed = { error: 'rate limited' };
  const inner = fakeInner([{ output: 'round 1' }, failed]);
  const vars = { feedback: '["a","b","c"]', draft: '<p/>' };
  const res = await chainRounds({ inner, prompt: 'p0', context: { vars }, buildPrompt });
  assert.strictEqual(res, failed);
  assert.strictEqual(inner.calls.length, 2);
});

test('a rerun reads every round from the CLI cache; another repeat or model does not', async () => {
  const store = new Map();
  const getCache = async () => ({ get: async (k) => store.get(k), set: async (k, v) => { store.set(k, v); } });
  const vars = { feedback: '["a","b"]', draft: '<p/>' };
  const run = (inner, cacheScope, repeatIndex = 0) =>
    chainRounds({ inner, cacheScope, prompt: 'p0', context: { vars, getCache, repeatIndex }, buildPrompt });

  const first = fakeInner([{ output: 'round 1' }, { output: 'round 2' }]);
  await run(first, 'luna');

  const rerun = fakeInner([]);
  const res = await run(rerun, 'luna');
  assert.strictEqual(rerun.calls.length, 0);
  assert.strictEqual(res.output, 'round 2');
  assert.strictEqual(res.cached, true);

  for (const [scope, repeat] of [['medium', 0], ['luna', 1]]) {
    const other = fakeInner([{ output: 'x' }, { output: 'y' }]);
    await run(other, scope, repeat);
    assert.strictEqual(other.calls.length, 2, `${scope} repeat ${repeat}`);
  }
});

test('an error is not cached', async () => {
  const store = new Map();
  const getCache = async () => ({ get: async (k) => store.get(k), set: async (k, v) => { store.set(k, v); } });
  const context = { vars: {}, getCache };
  await chainRounds({ inner: fakeInner([{ error: 'timeout' }]), cacheScope: 'luna', prompt: 'p0', context, buildPrompt });
  const retry = fakeInner([{ output: 'ok' }]);
  const res = await chainRounds({ inner: retry, cacheScope: 'luna', prompt: 'p0', context, buildPrompt });
  assert.strictEqual(res.output, 'ok');
});
