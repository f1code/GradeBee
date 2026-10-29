// report_tweak.go finds and replaces one paragraph of a report's HTML for a
// paragraph tweak.
package handler

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	nethtml "golang.org/x/net/html"
)

// paragraphSpan is the byte range of one p or li block's content, between its tags.
type paragraphSpan struct {
	tag        string
	start, end int
}

// reportParagraphs lists the p and li blocks in document order, the order
// the viewer's querySelectorAll('p, li') numbers them. Offsets index the raw
// string, so a splice leaves every other byte as stored. A block without an
// end tag has end -1.
func reportParagraphs(doc string) []paragraphSpan {
	z := nethtml.NewTokenizer(strings.NewReader(doc))
	var spans []paragraphSpan
	var open []int // indexes into spans awaiting their end tag
	pos := 0
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			return spans
		}
		n := len(z.Raw())
		name, _ := z.TagName()
		tag := string(name)
		if tag == "p" || tag == "li" {
			switch tt {
			case nethtml.StartTagToken:
				spans = append(spans, paragraphSpan{tag: tag, start: pos + n, end: -1})
				open = append(open, len(spans)-1)
			case nethtml.EndTagToken:
				for i := len(open) - 1; i >= 0; i-- {
					if spans[open[i]].tag == tag {
						spans[open[i]].end = pos
						open = append(open[:i], open[i+1:]...)
						break
					}
				}
			}
		}
		pos += n
	}
}

var errParagraphNotFound = errors.New("paragraph not found in the report")

// ReportParagraph returns the inner HTML of block i, as BuildReportPrompt
// shows it to the model.
func ReportParagraph(doc string, i int) (string, error) {
	sp, err := paragraphAt(doc, i)
	if err != nil {
		return "", err
	}
	return doc[sp.start:sp.end], nil
}

func paragraphAt(doc string, i int) (paragraphSpan, error) {
	spans := reportParagraphs(doc)
	if i < 0 || i >= len(spans) || spans[i].end < 0 {
		return paragraphSpan{}, errParagraphNotFound
	}
	return spans[i], nil
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

// ParagraphText is a block's text with whitespace collapsed, as the viewer
// reads textContent.
func ParagraphText(inner string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(inner, ""))), " ")
}

// checkParagraph confirms block i starts with the excerpt the viewer showed:
// the viewer numbers blocks in the sanitized, browser-parsed DOM, which can
// differ from the stored HTML, and a drifted index must not rewrite the wrong
// paragraph.
func checkParagraph(doc string, i int, excerpt string) error {
	inner, err := ReportParagraph(doc, i)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(ParagraphText(inner), strings.Join(strings.Fields(excerpt), " ")) {
		return errParagraphNotFound
	}
	return nil
}

var (
	wrappingBlock = regexp.MustCompile(`(?s)^<(p|li)\b[^>]*>(.*)</(p|li)>$`)
	blockTag      = regexp.MustCompile(`(?i)</?(p|li|ul|ol|h[1-6]|div|table)\b`)
)

// spliceParagraph replaces block i's content with the model's rewrite. A
// rewrite holding block tags is a whole report or several paragraphs: refused,
// since splicing it would duplicate the rest.
func spliceParagraph(doc string, i int, rewrite string) (string, error) {
	sp, err := paragraphAt(doc, i)
	if err != nil {
		return "", err
	}
	rewrite = strings.TrimSpace(rewrite)
	if m := wrappingBlock.FindStringSubmatch(rewrite); len(m) == 4 && m[1] == m[3] {
		rewrite = strings.TrimSpace(m[2])
	}
	if blockTag.MatchString(rewrite) || rewrite == "" {
		return "", fmt.Errorf("paragraph tweak: model returned more than one paragraph")
	}
	if !tagsBalanced(rewrite) {
		return "", fmt.Errorf("paragraph tweak: model returned unbalanced tags")
	}
	return doc[:sp.start] + rewrite + doc[sp.end:], nil
}

var voidTags = map[string]bool{"br": true, "wbr": true, "img": true}

// tagsBalanced reports whether every inline tag the rewrite opens it also
// closes, in order: an unclosed <b> would run on through the rest of the report.
func tagsBalanced(fragment string) bool {
	z := nethtml.NewTokenizer(strings.NewReader(fragment))
	var open []string
	for {
		switch z.Next() {
		case nethtml.ErrorToken:
			return len(open) == 0
		case nethtml.StartTagToken:
			if name, _ := z.TagName(); !voidTags[string(name)] {
				open = append(open, string(name))
			}
		case nethtml.EndTagToken:
			name, _ := z.TagName()
			if len(open) == 0 || open[len(open)-1] != string(name) {
				return false
			}
			open = open[:len(open)-1]
		}
	}
}
