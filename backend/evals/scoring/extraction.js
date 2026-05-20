/**
 * extraction.js — custom promptfoo assertion for extraction quality.
 *
 * Scores on:
 *   - precision: (correctly extracted students) / (total extracted students)
 *   - recall:    (correctly extracted students) / (expected students)
 *   - voice_preservation: every must_quote_substring appears verbatim in quoted_text
 *
 * The assertion passes (score >= 0.7) if all three sub-scores pass their thresholds.
 *
 * Expected fixture shape (expected.json):
 * {
 *   "expected_students": [
 *     {
 *       "name": "Alice",
 *       "class": "Grade 3A",
 *       "must_quote_substrings": ["did great in math today"]
 *     }
 *   ],
 *   "must_not_extract": ["The principal stopped by"]
 * }
 *
 * @param {string} output - Raw LLM output string (JSON from extraction endpoint)
 * @param {{ expected: object, metric: string }} context - From promptfoo config
 */
module.exports = async (output, context) => {
  const config = context.config || {};
  const expected = config.expected;

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
      if (!ext.quoted_text || !ext.quoted_text.includes(substring)) {
        voiceScore = 0;
        reasons.push(`FAIL: "${substring}" missing from quoted_text for ${exp.name}`);
      }
    }
  }
  totalScore += voiceScore;
  numMetrics++;

  // --- Must-not-extract check ---
  for (const forbidden of mustNotExtract) {
    const leaked = extracted.some(
      (ext) => ext.quoted_text && ext.quoted_text.toLowerCase().includes(forbidden.toLowerCase())
    );
    if (leaked) {
      totalScore -= 0.5;
      reasons.push(`FAIL: forbidden content "${forbidden}" leaked into output`);
    }
  }

  const avgScore = numMetrics > 0 ? Math.max(0, totalScore / numMetrics) : 0;
  const pass = precision >= 0.7 && recall >= 0.7 && voiceScore === 1;

  return {
    pass,
    score: avgScore,
    reason: reasons.join('; '),
  };
};
