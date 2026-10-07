// Runs from backend/ `make test`.
const test = require('node:test');
const assert = require('node:assert');
const transform = require('./report-wordcounts.js');
const { sectionWordCounts } = transform;

test('counts words per h3 section, tags and entities stripped', () => {
  const html = '<h3>Motivation &amp; Participation</h3>\n<p>She <b>takes</b> part &amp; helps.</p>\n'
    + '<h3>Markers</h3><ul><li>No&nbsp;markers</li></ul>';
  assert.deepStrictEqual(sectionWordCounts(html), [
    { heading: 'Motivation & Participation', words: 4 },
    { heading: 'Markers', words: 2 },
  ]);
});

test('an out-of-range numeric entity is left as is', () => {
  assert.deepStrictEqual(sectionWordCounts('<h3>A</h3><p>x &#99999999; &#x41;</p>'), [{ heading: 'A', words: 3 }]);
});

test('text before the first heading is its own section; an empty one is dropped', () => {
  assert.deepStrictEqual(sectionWordCounts('<p>Intro line.</p><h3>A</h3><p>one</p>'), [
    { heading: '(before first heading)', words: 2 },
    { heading: 'A', words: 1 },
  ]);
  assert.deepStrictEqual(sectionWordCounts('<h3>A</h3><p></p>'), [{ heading: 'A', words: 0 }]);
});

test('no headings: the whole report is one section', () => {
  assert.deepStrictEqual(sectionWordCounts('<p>Three short words</p>'), [
    { heading: '(before first heading)', words: 3 },
  ]);
});

test('transform keeps the output and appends a delimited block with a total', () => {
  const out = '<h3>A</h3><p>one two</p><h3>B</h3><p>three</p>';
  const r = transform(out);
  assert.ok(r.startsWith(out));
  assert.match(r, /<word_counts>\n.*not part of the report\.\n- A: 2 words\n- B: 1 words\n- Total: 3 words\n<\/word_counts>$/);
});
