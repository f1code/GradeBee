/**
 * assemble.js — promptfoo output transform for the two-pass extraction
 * contract (#125).
 *
 * Pass 2 returns passages: kind, the names the teacher spoke, the child they
 * resolve to, and a summary. `scoring/extraction.js` grades notes — one entry
 * per child with the text their note would hold. This is the step between,
 * and it is the JavaScript twin of `guardPassages` (backend/extract.go) and
 * `assemblePassages` (backend/voice_note_passages.go). Change one, change both,
 * or the eval stops grading what production does.
 *
 * Why a transform rather than a second scorer: the four scoring axes and every
 * fixture's `expected.json` describe notes, and they are unchanged by this
 * contract. Rewriting them to speak passages would have thrown away the
 * comparison with everything measured before.
 *
 * Two deliberate divergences from production, both forced by promptfoo owning
 * the model call:
 *
 *  - Pass 1 does not run. promptfoo makes one call per test, so the fixture
 *    names the class pass 1 is taken to have pinned, in `vars.class_name`.
 *    Pass 1 measured 93/93 on this model over 31 samples; the case it exists
 *    for is the decline, which this contract does not have (#127).
 *  - `student` is a plain string in the eval's schema, not an enum of the
 *    class roster — a static provider config cannot template a per-fixture
 *    roster. So `student` is resolved here by folded exact match against the
 *    roster, which is what the enum guarantees structurally in production. A
 *    spoken name that fits nobody stays unattributed either way.
 */

/** foldName mirrors backend/match.go: lowercase, strip accents, drop
 * everything that is not a letter or digit. */
function foldName(s) {
  return (s || '')
    .toLowerCase()
    .normalize('NFD')
    .replace(/[^\p{Letter}\p{Number}]/gu, '');
}

/** labelStopList mirrors backend/match.go: folded labels that are never a
 * name. The guard below reads it, so a passage the model labelled "She" is
 * not a named passage however confidently it named a child. */
const labelStopList = new Set([
  'he', 'him', 'his',
  'she', 'her', 'hers',
  'they', 'them', 'their', 'theirs',
  'it', 'its',
  'i', 'me', 'my', 'mine',
  'we', 'us', 'our', 'ours',
  'you', 'your', 'yours',
  'this', 'that', 'these', 'those',
  'who', 'someone', 'somebody', 'anyone', 'anybody',
  'everyone', 'everybody', 'nobody', 'noone',
  'all', 'both', 'one', 'and',
]);

function hasSpokenName(labels) {
  return (labels || []).some((l) => {
    const key = foldName(l);
    return key !== '' && !labelStopList.has(key);
  });
}

/** guardPassages: a child passage with no spoken name is unknown, whoever the
 * model named. In every roster phantom measured across 280 runs the model had
 * labelled the block with a pronoun. */
function guardPassages(passages) {
  return passages.map((p) => {
    if (p.kind === 'child' && !hasSpokenName(p.spoken_labels)) {
      return { ...p, kind: 'unknown', spoken_labels: [], student: '' };
    }
    return p;
  });
}

/** assemble: one entry per child the recording reached, group passages last. */
function assemble(passages, roster) {
  const byFolded = new Map(roster.map((s) => [foldName(s.name), s.name]));
  const notes = [];
  const at = new Map();
  const group = [];

  for (const p of passages) {
    if (p.kind === 'none') continue;
    if (p.kind === 'group') {
      group.push(p.summary);
      continue;
    }
    if (p.kind !== 'child') continue;

    const name = byFolded.get(foldName(p.student));
    if (!name) continue; // reached nobody: the unattributed list, never a note

    if (!at.has(name)) {
      at.set(name, notes.length);
      notes.push({ name, quoted_text: p.summary });
      continue;
    }
    // Blank line between passages: separate stretches of speech, and running
    // them together invents a sentence the teacher never said.
    notes[at.get(name)].quoted_text += `\n\n${p.summary}`;
  }

  for (const g of group) {
    for (const n of notes) n.quoted_text += `\n\n${g}`;
  }
  return notes;
}

module.exports = (output, context) => {
  const parsed = typeof output === 'string' ? JSON.parse(output) : output;
  const className = context.vars.class_name;
  const classes = typeof context.vars.classes === 'string'
    ? JSON.parse(context.vars.classes)
    : context.vars.classes;

  const pinned = (classes || []).find((c) => c.name === className);
  if (!pinned) {
    throw new Error(`assemble.js: vars.class_name ${JSON.stringify(className)} is not one of vars.classes`);
  }

  const passages = guardPassages(parsed.observations || []);
  return {
    students: assemble(passages, pinned.students || []).map((n) => ({
      ...n,
      class_name: className,
    })),
  };
};
