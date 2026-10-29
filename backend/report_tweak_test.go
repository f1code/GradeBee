package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tweakDoc = "<h3>Reading</h3>\n<p>Reads &amp; writes <b>well</b>.</p>\n" +
	"<ul>\n  <li><p>Nested one</p></li>\n  <li>Two  [MISSING]</li>\n</ul>\n<p>Last</p>"

func TestReportParagraphs_NumbersLikeQuerySelectorAll(t *testing.T) {
	var got []string
	for i := range reportParagraphs(tweakDoc) {
		inner, err := ReportParagraph(tweakDoc, i)
		require.NoError(t, err)
		got = append(got, ParagraphText(inner))
	}
	assert.Equal(t, []string{"Reads & writes well.", "Nested one", "Nested one", "Two [MISSING]", "Last"}, got)

	_, err := ReportParagraph(tweakDoc, 5)
	assert.ErrorIs(t, err, errParagraphNotFound)
	_, err = ReportParagraph("<p>open", 0)
	assert.ErrorIs(t, err, errParagraphNotFound, "no end tag")
}

func TestSpliceParagraph_KeepsEveryOtherByte(t *testing.T) {
	got, err := spliceParagraph(tweakDoc, 3, "<li>Two, padded.</li>\n")
	require.NoError(t, err)
	assert.Equal(t, "<h3>Reading</h3>\n<p>Reads &amp; writes <b>well</b>.</p>\n"+
		"<ul>\n  <li><p>Nested one</p></li>\n  <li>Two, padded.</li>\n</ul>\n<p>Last</p>", got)

	_, err = spliceParagraph(tweakDoc, 3, "<p>Two.</p><p>Three.</p>")
	assert.Error(t, err, "several paragraphs would duplicate the report")
	_, err = spliceParagraph(tweakDoc, 3, "  ")
	assert.Error(t, err)
	_, err = spliceParagraph(tweakDoc, 3, "Reads <b>well")
	assert.Error(t, err, "an unclosed tag would run on through the report")
	_, err = spliceParagraph(tweakDoc, 3, "<b>Reads <i>well</b></i>")
	assert.Error(t, err, "misnested")
	_, err = spliceParagraph(tweakDoc, 3, "Reads <b>well</b>.<br>Writes too.")
	assert.NoError(t, err)
}

func TestCheckParagraph_RejectsDriftedIndex(t *testing.T) {
	assert.NoError(t, checkParagraph(tweakDoc, 0, "Reads & writes  well."))
	assert.ErrorIs(t, checkParagraph(tweakDoc, 4, "Reads & writes"), errParagraphNotFound)
}

func TestBuildReportPrompt_Tweak(t *testing.T) {
	i := 3
	notes := []Note{{Date: "2026-02-01", Summary: "edited", UpdatedAt: "2026-05-01T00:00:00Z"}}
	prompt := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", &ReportRevision{
		Draft:           tweakDoc,
		DraftCreatedAt:  "2026-04-01T00:00:00Z",
		EarlierFeedback: []string{"warmer"},
		Feedback:        "about 300 characters",
		Paragraph:       &i,
	})
	assert.Contains(t, prompt, reportTweakDraftHeader+tweakDoc)
	assert.Contains(t, prompt, reportEarlierFeedbackHeader+"Round 1: warmer\n")
	assert.Contains(t, prompt, reportTweakParagraphHeader+"Two  [MISSING]\n\n"+reportTweakAskHeader+"about 300 characters\n\n"+reportTweakFooter)
	assert.Contains(t, prompt, "- 2026-02-01: edited\n", "a tweak tags no note (new)")
	assert.NotContains(t, prompt, reportPrevDraftHeader)
	assert.NotContains(t, prompt, reportTaskFooter)
}

// TestHandleRegenerateReport_Tweak rewrites one list item: the rest of the
// report is byte-identical, the row carries the tweak hash, and a later round
// reads the ask scoped to its paragraph.
func TestHandleRegenerateReport_Tweak(t *testing.T) {
	ctx := context.Background()
	f := newRegenFixture(t, func(db *sql.DB, _ int64, first *Report) {
		_, err := db.ExecContext(ctx, `UPDATE reports SET html = ? WHERE id = ?`, tweakDoc, first.ID)
		require.NoError(t, err)
	})

	tweak := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/reports/%d/regenerate", f.first.ID), bytes.NewReader([]byte(body)))
		req.SetPathValue("id", itoa(f.first.ID))
		rec := httptest.NewRecorder()
		handleRegenerateReport(rec, clerkReq(req, "user_abc"))
		return rec
	}

	rec := tweak(`{"feedback":"pad it","paragraph":3,"excerpt":"Reads"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "excerpt names another paragraph")
	rec = tweak(`{"feedback":"pad it","paragraph":3}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "no excerpt")

	rec = tweak(`{"feedback":"pad it","paragraph":3,"excerpt":"Two [MISS"}`)
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	var res ReportResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&res))
	assert.Equal(t, "<h3>Reading</h3>\n<p>Reads &amp; writes <b>well</b>.</p>\n"+
		"<ul>\n  <li><p>Nested one</p></li>\n  <li>next</li>\n</ul>\n<p>Last</p>", res.HTML)
	saved, err := f.reportRepo.GetByID(ctx, res.ID)
	require.NoError(t, err)
	assert.Equal(t, res.HTML, saved.HTML)
	assert.Equal(t, ReportTweakPromptHash, *saved.PromptHash)
	assert.NotEqual(t, ReportPromptHash, ReportTweakPromptHash)

	_, prompt := f.regenerate(res.ID, "shorter")
	assert.Contains(t, prompt, reportEarlierFeedbackHeader+`Round 1: In the paragraph starting "Two [MISSING]": pad it`+"\n")
}
