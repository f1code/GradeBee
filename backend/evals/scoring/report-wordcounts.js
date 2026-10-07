// Assertion transform for the report rubric: appends per-section word counts
// to the judge's copy of the output. The judge cannot count (it said 63 for a
// section of 83), so code measures and the judge reads the length rule from
// the instructions. No length rule lives here.
//
// A transform, not a nunjucks filter: promptfoo 0.121 renders rubricPrompt and
// assertion values with bare nunjucks, so config nunjucksFilters never reach
// the grading prompt.

const ENTITIES = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ' };

function text(html) {
  return html
    .replace(/<[^>]*>/g, ' ')
    .replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (m, e) => {
      if (e[0] === '#') {
        const cp = e[1].toLowerCase() === 'x' ? parseInt(e.slice(2), 16) : Number(e.slice(1));
        // A throw here would error the row and block pinning.
        return cp <= 0x10ffff ? String.fromCodePoint(cp) : m;
      }
      return ENTITIES[e.toLowerCase()] ?? m;
    });
}

// A lone "&" or "-" is not a word.
const countWords = (s) => s.split(/\s+/).filter((w) => /[\p{L}\p{N}]/u.test(w)).length;

// Sections split on <h1>-<h6>; text before the first heading counts as its own
// section, when it has any words.
function sectionWordCounts(html) {
  const sections = [];
  const re = /<h[1-6][^>]*>([\s\S]*?)<\/h[1-6]>/gi;
  let heading = null;
  let start = 0;
  const push = (end) => {
    const words = countWords(text(html.slice(start, end)));
    if (heading !== null || words > 0) sections.push({ heading: heading ?? '(before first heading)', words });
  };
  for (let m; (m = re.exec(html)); ) {
    push(m.index);
    heading = text(m[1]).replace(/\s+/g, ' ').trim();
    start = re.lastIndex;
  }
  push(html.length);
  return sections;
}

function wordCountBlock(html) {
  const sections = sectionWordCounts(html);
  const total = sections.reduce((n, s) => n + s.words, 0);
  const lines = sections.map((s) => `- ${s.heading}: ${s.words} words`);
  return [
    '<word_counts>',
    'Measured by code from the report above; not part of the report.',
    ...lines,
    `- Total: ${total} words`,
    '</word_counts>',
  ].join('\n');
}

module.exports = (output) => `${output}\n\n${wordCountBlock(String(output))}`;
module.exports.sectionWordCounts = sectionWordCounts;
