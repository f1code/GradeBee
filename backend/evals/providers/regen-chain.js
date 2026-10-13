'use strict';
// Wraps a report model so a regenerate case runs every feedback round on it:
// round 1 revises the case's draft, each later round the previous round's
// output. eval-cli builds every prompt; promptfoo grades the last output only.
// A case without feedback is one plain call.

const { execFile } = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { promisify } = require('util');

const run = promisify(execFile);
const EVAL_CLI = path.resolve(__dirname, '../../bin/eval-cli');

class RegenChainProvider {
  constructor(options) {
    this.config = options.config;
    this.label = options.label;
    if (!this.config?.inner?.id) {
      throw new Error('regen-chain: config.inner.id is required');
    }
  }

  id() {
    return this.config.inner.id;
  }

  async innerProvider() {
    // Loaded here, not at the top: the unit test needs no promptfoo.
    this.inner ??= require('promptfoo').loadApiProvider(this.config.inner.id, {
      options: { config: this.config.inner.config ?? {} },
    });
    return this.inner;
  }

  async callApi(prompt, context) {
    return chainRounds({
      inner: await this.innerProvider(),
      cacheScope: JSON.stringify(this.config.inner),
      prompt,
      context,
      buildPrompt,
    });
  }
}

// chainRounds calls inner once per feedback round: the first prompt as given,
// each later one built with the previous output as the draft. An error ends
// the chain and is returned as is.
async function chainRounds({ inner, cacheScope, prompt, context, buildPrompt }) {
  const vars = resolveFileVars(context?.vars ?? {});
  const rounds = vars.feedback ? JSON.parse(vars.feedback).length : 0;

  let res = await cachedCall(inner, cacheScope, prompt, context);
  const total = { cost: res.cost ?? 0, tokenUsage: { ...res.tokenUsage } };
  const earlier = [];
  for (let round = 1; round < rounds && !res.error; round++) {
    earlier.push(res.output);
    const roundVars = { ...vars, draft: res.output, round };
    const next = await buildPrompt(roundVars);
    res = await cachedCall(inner, cacheScope, next, { ...context, vars: roundVars });
    total.cost += res.cost ?? 0;
    addUsage(total.tokenUsage, res.tokenUsage);
  }
  if (rounds <= 1 || res.error) return res;
  return {
    ...res,
    cost: total.cost,
    tokenUsage: total.tokenUsage,
    metadata: { ...res.metadata, earlierRounds: earlier },
  };
}

// cachedCall caches through the CLI's cache (context.getCache), never the
// inner provider's: require('promptfoo') here is a second copy holding its own
// view of the same cache file, and the CLI rewrites that file without the
// copy's entries. Under the CLI's --no-cache that cache lives one run; keys
// carry repeatIndex, so --repeat still samples.
async function cachedCall(inner, cacheScope, prompt, context) {
  const cache = context?.getCache ? await context.getCache() : null;
  const key = 'regen-chain:' + crypto.createHash('sha256')
    .update(JSON.stringify([cacheScope, context?.repeatIndex ?? 0, prompt]))
    .digest('hex');
  const hit = cache && await cache.get(key);
  if (hit) return { ...hit, cached: true };
  const res = await inner.callApi(prompt, { ...context, bustCache: true });
  if (cache && !res.error) await cache.set(key, res);
  return res;
}

// promptfoo hands providers file:// vars unread; eval-cli needs their content.
// Paths resolve from the working directory, the config's (make cds there).
function resolveFileVars(vars) {
  const out = {};
  for (const [k, v] of Object.entries(vars)) {
    out[k] = typeof v === 'string' && v.startsWith('file://')
      ? fs.readFileSync(v.slice('file://'.length), 'utf8')
      : v;
  }
  return out;
}

async function buildPrompt(vars) {
  const req = JSON.stringify({ vars, config: { task: 'build-report-prompt' } });
  const { stdout } = await run(EVAL_CLI, [req], { maxBuffer: 16 * 1024 * 1024 });
  return stdout.trim();
}

function addUsage(sum, usage) {
  for (const [k, v] of Object.entries(usage ?? {})) {
    if (typeof v === 'number') sum[k] = (sum[k] ?? 0) + v;
  }
}

module.exports = RegenChainProvider;
module.exports.chainRounds = chainRounds;
