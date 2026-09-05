/**
 * extraction.js — custom promptfoo assertion for extraction quality.
 *
 * Scores on:
 *   - precision: (correctly extracted students) / (total extracted students)
 *   - recall:    (correctly extracted students) / (expected students)
 *   - voice_preservation: every must_quote_substring appears verbatim in quoted_text
 *   - attribution: no must_not_quote_substring appears in that student's quoted_text
 *   - preference: how many should_quote_substrings appear (soft — see below)
 *
 * The assertion passes only if all four *hard* sub-scores pass their thresholds
 * and no must_not_extract phrase appears in any student's quoted_text.
 *
 * should_quote_substrings is the soft counterpart of must_quote_substrings: text
 * that makes a note better but whose absence is not a defect. It scores as the
 * fraction matched and moves the row's score, and it is deliberately absent from
 * the pass expression, so a run that drops it scores lower and still passes.
 * Taught vocabulary is the case it exists for (#121): "He was doing good with
 * making the full sentences. Yes, I can. No, I can't." says which structure,
 * where the first sentence alone does not — but the model reaches it only some
 * runs, and the run that misses it is not a failing recording. evals/README.md
 * owns the measured rate; do not restate it here, it drifted once already.
 *
 * The axis is skipped entirely for fixtures that define no should_quote_substrings,
 * so adding it moved no other row's score.
 *
 * must_not_quote_substrings is the per-student counterpart of the global
 * must_not_extract: it catches cross-student bleed, where one student's
 * quoted_text swallows an observation the teacher made about a different
 * student. precision/recall only compare the *set* of extracted names, so
 * without this the whole transcript can land under every student and still
 * score 1.00.
 *
 * Expected fixture shape (expected.json):
 * {
 *   "expected_students": [
 *     {
 *       "name": "Alice",
 *       "class": "Grade 3A",
 *       "must_quote_substrings": ["did great in math today"],
 *       "must_not_quote_substrings": ["Bob was quiet"],
 *       "should_quote_substrings": ["Yes, I can"]
 *     }
 *   ],
 *   "must_not_extract": ["The principal stopped by"]
 * }
 *
 * @param {string} output - Raw LLM output string (JSON from extraction endpoint)
 * @param {{ expected: object, metric: string }} context - From promptfoo config
 */
/**
 * quoteMatches reports whether substring is present in text. `/pattern/flags`
 * is a regex; anything else is a literal substring, so a phrase carrying regex
 * metacharacters ("couldn't do.") matches as written.
 *
 * A pattern that will not compile counts as "not present" rather than throwing.
 * The soft axis must never be able to error a run, and a fixture typo should
 * show up as a miss to investigate, not as a crashed assertion.
 */
function quoteMatches(substring, text) {
  const reMatch = substring.match(/^\/(.+)\/([gimsuy]*)$/);
  if (!reMatch) return (text || '').includes(substring);
  try {
    return new RegExp(reMatch[1], reMatch[2]).test(text || '');
  } catch (e) {
    return false;
  }
}

module.exports = async (output, context) => {
  const config = context.config || {};
  let expected = config.expected;

  // promptfoo does not auto-resolve file:// references in assertion config blocks.
  // Load and parse the file manually when the value is a file:// path.
  if (typeof expected === 'string' && expected.startsWith('file://')) {
    const path = require('path');
    const fs = require('fs');
    const filePath = path.resolve(__dirname, '..', expected.slice('file://'.length));
    try {
      expected = JSON.parse(fs.readFileSync(filePath, 'utf8'));
    } catch (e) {
      return { pass: false, score: 0, reason: `Could not load expected fixture ${filePath}: ${e.message}` };
    }
  } else if (typeof expected === 'string') {
    try { expected = JSON.parse(expected); } catch (e) {
      return { pass: false, score: 0, reason: `Could not parse expected fixture: ${e.message}` };
    }
  }

  if (!expected) {
    return {
      pass: false,
      score: 0,
      reason: 'No expected fixture provided in config',
    };
  }

  let parsed;
  try {
    parsed = typeof output === 'string' ? JSON.parse(output) : output;
  } catch (e) {
    return {
      pass: false,
      score: 0,
      reason: `Output is not valid JSON: ${e.message}`,
    };
  }

  const extracted = parsed.students || [];
  const expectedStudents = expected.expected_students || [];
  const mustNotExtract = expected.must_not_extract || [];

  const reasons = [];
  let totalScore = 0;
  let numMetrics = 0;

  // --- Precision / Recall ---
  const normalise = (s) => s.toLowerCase().replace(/\s+/g, ' ').trim();

  let truePositives = 0;
  for (const ext of extracted) {
    const match = expectedStudents.find(
      (e) => normalise(e.name) === normalise(ext.name)
    );
    if (match) truePositives++;
  }

  const precision = extracted.length > 0 ? truePositives / extracted.length : (expectedStudents.length === 0 ? 1 : 0);
  const recall = expectedStudents.length > 0 ? truePositives / expectedStudents.length : 1;

  reasons.push(`precision=${precision.toFixed(2)} (${truePositives}/${extracted.length})`);
  reasons.push(`recall=${recall.toFixed(2)} (${truePositives}/${expectedStudents.length})`);
  totalScore += precision + recall;
  numMetrics += 2;

  // --- Voice preservation ---
  let voiceScore = 1;
  for (const exp of expectedStudents) {
    const ext = extracted.find((e) => normalise(e.name) === normalise(exp.name));
    if (!ext) continue; // already counted in recall

    for (const substring of exp.must_quote_substrings || []) {
      if (!quoteMatches(substring, ext.quoted_text)) {
        voiceScore = 0;
        reasons.push(`FAIL: "${substring}" missing from quoted_text for ${exp.name}`);
      }
    }
  }
  totalScore += voiceScore;
  numMetrics++;

  // --- Attribution (no cross-student bleed) ---
  let attributionScore = 1;
  for (const exp of expectedStudents) {
    const ext = extracted.find((e) => normalise(e.name) === normalise(exp.name));
    if (!ext) continue; // already counted in recall

    for (const substring of exp.must_not_quote_substrings || []) {
      if (quoteMatches(substring, ext.quoted_text)) {
        attributionScore = 0;
        reasons.push(`FAIL: "${substring}" leaked into quoted_text for ${exp.name}`);
      }
    }
  }
  totalScore += attributionScore;
  numMetrics++;

  // Everything above this line decides pass; the soft axis below only informs.
  // The must-not-extract penalty further down is a hard signal and is applied
  // to both totals, so `hard` cannot look clean on a run that leaked.
  let hardTotal = totalScore;
  const hardMetrics = numMetrics;

  // --- Preference (soft): text that improves a note but is not required ---
  // Scored as the fraction matched, and kept out of `pass` on purpose. Counted
  // only when a fixture asks for it, so rows that do not are unaffected.
  let wantedTotal = 0;
  let wantedFound = 0;
  const missedPreferred = [];
  for (const exp of expectedStudents) {
    const ext = extracted.find((e) => normalise(e.name) === normalise(exp.name));
    for (const substring of exp.should_quote_substrings || []) {
      wantedTotal++;
      // A student with no note at all still counts against the fraction, and
      // still says which phrase went missing — recall explains why, but a bare
      // number with no diagnostic is what sent the last measurement wrong.
      if (!ext) {
        missedPreferred.push(`"${substring}" for ${exp.name} (no note)`);
        continue;
      }
      if (quoteMatches(substring, ext.quoted_text)) wantedFound++;
      else missedPreferred.push(`"${substring}" for ${exp.name}`);
    }
  }
  if (wantedTotal > 0) {
    const preferenceScore = wantedFound / wantedTotal;
    reasons.push(`preferred=${preferenceScore.toFixed(2)} (${wantedFound}/${wantedTotal})`);
    if (missedPreferred.length > 0) {
      reasons.push(`MISSING (not a failure): ${missedPreferred.join(', ')}`);
    }
    totalScore += preferenceScore;
    numMetrics++;
  }

  // --- Must-not-extract check ---
  // Global: content nobody owns must land in no entry. The per-student
  // attribution axis only sees leaks into an *expected* student, so a
  // fixture with one expected student needs this axis to fail the test.
  let forbiddenLeaked = false;
  for (const forbidden of mustNotExtract) {
    const leaked = extracted.some(
      (ext) => ext.quoted_text && ext.quoted_text.toLowerCase().includes(forbidden.toLowerCase())
    );
    if (leaked) {
      forbiddenLeaked = true;
      totalScore -= 0.5;
      hardTotal -= 0.5;
      reasons.push(`FAIL: forbidden content "${forbidden}" leaked into output`);
    }
  }

  const avgScore = numMetrics > 0 ? Math.max(0, totalScore / numMetrics) : 0;
  const pass = precision >= 0.7 && recall >= 0.7 && voiceScore === 1 && attributionScore === 1 && !forbiddenLeaked;

  // `hard` is the score with the soft axis taken out, and it is what
  // scripts/diff-baseline.js counts as a regression or an improvement. Without
  // it a preferred phrase the model reaches only some runs swings the row by
  // more than the differ's ±0.05 band, so the summary line would announce a
  // regression nobody caused. The table still shows the real score.
  const hardScore = hardMetrics > 0 ? Math.max(0, hardTotal / hardMetrics) : 0;

  return {
    pass,
    score: avgScore,
    namedScores: { hard: hardScore },
    reason: reasons.join('; '),
  };
};
