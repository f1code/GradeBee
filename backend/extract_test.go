package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Live extraction tests (those that call a real LLM) live in llm_live_test.go
// behind the `llm` build tag; this file holds only offline checks.

// schemaProp mirrors the subset of JSON Schema shape the two passes produce.
type schemaProp struct {
	Type       string                `json:"type"`
	Enum       []string              `json:"enum,omitempty"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
	Items      *schemaProp           `json:"items,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

func testClasses() []ClassGroup {
	return []ClassGroup{
		{Name: "Period 3", Students: []ClassStudent{{Name: "Maxence"}, {Name: "Côme", Aliases: []string{"Colm"}}}},
		{Name: "Period 5", Students: []ClassStudent{{Name: "Amara"}}},
	}
}

// TestClassPickSchemaAllowsTheDecline: pass 1's enum is the teacher's class
// names plus "", and that one value is the whole of #127's model-facing change.
// classPickPromptSuffix has always told the model to return "" when the header
// names no single class; until this value existed the instruction could not be
// obeyed.
//
// It goes last, which is where the measured probe put it
// (research/2026-09-05-123-summaries-vs-spans, classNamesEnum).
func TestClassPickSchemaAllowsTheDecline(t *testing.T) {
	var schema schemaProp
	require.NoError(t, json.Unmarshal(classPickSchema(testClasses()), &schema))

	assert.Equal(t, []string{"Period 3", "Period 5", ""}, schema.Properties["class_name"].Enum)
	assert.Equal(t, []string{"class_name"}, schema.Required)
}

// TestPassageSchemaScopesStudentToOneClass: the student enum is one class's
// roster plus "". A child of another class must not be reachable — first names
// repeat across a teacher's classes, and that is what pass 1 exists to
// disambiguate.
func TestPassageSchemaScopesStudentToOneClass(t *testing.T) {
	var schema schemaProp
	require.NoError(t, json.Unmarshal(passageSchema(testClasses()[0]), &schema))

	items := schema.Properties["observations"].Items
	require.NotNil(t, items)
	assert.Equal(t, []string{"Maxence", "Côme", ""}, items.Properties["student"].Enum)
	assert.NotContains(t, items.Properties["student"].Enum, "Amara", "pass 2 must not be able to reach another class's child")
	assert.Equal(t, []string{"child", "unknown", "group", "none"}, items.Properties["kind"].Enum)
	assert.Equal(t, []string{"kind", "spoken_labels", "student", "summary"}, items.Required)
}

// TestPassageSchemaKeepsPropertyOrder: under structured output the schema's
// property order is the model's generation order, and spoken_labels coming
// before student is what stops the model choosing a child and then inventing a
// label to justify it. encoding/json sorts map keys, so this would silently
// break the day a property was renamed to something sorting differently.
func TestPassageSchemaKeepsPropertyOrder(t *testing.T) {
	raw := string(passageSchema(testClasses()[0]))

	order := []string{`"kind"`, `"spoken_labels"`, `"student"`, `"summary"`}
	at := -1
	for _, key := range order {
		i := strings.Index(raw, key)
		require.NotEqual(t, -1, i, "schema is missing %s", key)
		assert.Greater(t, i, at, "%s is out of generation order in %s", key, raw)
		at = i
	}
}

// TestBuildPassagePromptListsOneClass: the roster in pass 2's prompt is one
// class's children, aliases read as "also called", and no other class appears.
func TestBuildPassagePromptListsOneClass(t *testing.T) {
	got := BuildPassagePrompt(testClasses()[0])

	assert.True(t, strings.HasPrefix(got, passagePromptPrefix), "the measured rules must open the prompt unchanged")
	assert.Contains(t, got, "- Maxence\n")
	assert.Contains(t, got, "- Côme (also called Colm)\n")
	assert.NotContains(t, got, "Amara", "pass 2 sees one class")
	assert.NotContains(t, got, "Period 5", "pass 2 is not told about the other classes at all")
}

// TestBuildClassPickPromptListsClassesOnly: pass 1 is shown class names and no
// children. A roster in front of the class question is what lets a name in the
// transcript outvote the spoken header.
func TestBuildClassPickPromptListsClassesOnly(t *testing.T) {
	got := BuildClassPickPrompt(testClasses())

	assert.Contains(t, got, "- Period 3\n")
	assert.Contains(t, got, "- Period 5\n")
	assert.True(t, strings.HasSuffix(got, classPickPromptSuffix), "the decline instruction must close the prompt unchanged")
	assert.NotContains(t, got, "Maxence")
	assert.NotContains(t, got, "Amara")
}

// TestGuardPassages covers the structural backstop under the prompt's
// no-elimination rules: in every roster phantom measured across 280 runs the
// model had labelled the block with a pronoun.
func TestGuardPassages(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   ExtractedPassage
		want ExtractedPassage
	}{
		{
			name: "a pronoun-only child passage is demoted",
			in:   ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{"She"}, Student: "Ombeline", Summary: "she knocked on the boxes"},
			want: ExtractedPassage{Kind: PassageUnknown, Summary: "she knocked on the boxes"},
		},
		{
			name: "every label a pronoun is still pronoun-only",
			in:   ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{"they", "Them"}, Student: "Ombeline", Summary: "x"},
			want: ExtractedPassage{Kind: PassageUnknown, Summary: "x"},
		},
		{
			name: "no label at all is demoted",
			in:   ExtractedPassage{Kind: PassageChild, Student: "Ombeline", Summary: "x"},
			want: ExtractedPassage{Kind: PassageUnknown, Summary: "x"},
		},
		{
			name: "one real name among pronouns passes",
			in:   ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{"She", "Ombeline"}, Student: "Ombeline", Summary: "x"},
			want: ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{"She", "Ombeline"}, Student: "Ombeline", Summary: "x"},
		},
		{
			name: "a name matching nobody still counts as a name",
			in:   ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{"Polly"}, Student: "", Summary: "x"},
			want: ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{"Polly"}, Student: "", Summary: "x"},
		},
		{
			name: "a group passage is left alone",
			in:   ExtractedPassage{Kind: PassageGroup, Summary: "everyone did well"},
			want: ExtractedPassage{Kind: PassageGroup, Summary: "everyone did well"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, []ExtractedPassage{tc.want}, guardPassages([]ExtractedPassage{tc.in}))
		})
	}
}

// twoPassProvider is an LLMProvider that answers the two extraction calls in
// order and records what each was asked.
type twoPassProvider struct {
	replies []string
	errs    []error
	calls   []ChatJSONRequest
}

var _ LLMProvider = (*twoPassProvider)(nil)

func (p *twoPassProvider) Name() string           { return "two-pass-fake" }
func (p *twoPassProvider) Model(_ LLMTask) string { return fakeExtractModel }

func (p *twoPassProvider) ChatJSON(_ context.Context, req ChatJSONRequest, out any) (string, error) {
	i := len(p.calls)
	p.calls = append(p.calls, req)
	if i < len(p.errs) && p.errs[i] != nil {
		return "", p.errs[i]
	}
	if i >= len(p.replies) {
		return "", errors.New("twoPassProvider: unexpected extra call")
	}
	return p.replies[i], json.Unmarshal([]byte(p.replies[i]), out)
}

func (p *twoPassProvider) ChatText(_ context.Context, _ ChatTextRequest) (string, error) {
	return "", errors.New("twoPassProvider: ChatText not expected on the extraction path")
}

func (p *twoPassProvider) Vision(_ context.Context, _ VisionRequest, _ any) (string, error) {
	return "", errors.New("twoPassProvider: Vision not expected on the extraction path")
}

func (p *twoPassProvider) Transcribe(_ context.Context, _ TranscribeRequest) (TranscribeResponse, error) {
	return TranscribeResponse{}, errors.New("twoPassProvider: Transcribe not expected on the extraction path")
}

// TestExtractRunsBothPasses: pass 1 names the class, pass 2 is scoped to it,
// and the guard runs on what comes back.
func TestExtractRunsBothPasses(t *testing.T) {
	provider := &twoPassProvider{replies: []string{
		`{"class_name":"Period 3"}`,
		`{"observations":[
			{"kind":"child","spoken_labels":["Colm"],"student":"Côme","summary":"read well"},
			{"kind":"child","spoken_labels":["She"],"student":"Maxence","summary":"she got on with it"}
		]}`,
	}}

	got, err := newLLMExtractor(provider).Extract(t.Context(), ExtractRequest{
		Transcript: "Period 3, Monday. Colm read well. She got on with it.",
		Classes:    testClasses(),
	})
	require.NoError(t, err)

	assert.Equal(t, "Period 3", got.ClassName)
	assert.Equal(t, []ExtractedPassage{
		{Kind: PassageChild, SpokenLabels: []string{"Colm"}, Student: "Côme", Summary: "read well"},
		// The guard demoted this one: the model named a child for a block whose
		// only label is a pronoun.
		{Kind: PassageUnknown, Summary: "she got on with it"},
	}, got.Passages)

	require.Len(t, provider.calls, 2, "extraction is two calls")
	assert.Equal(t, BuildClassPickPrompt(testClasses()), provider.calls[0].SystemPrompt)
	assert.Equal(t, BuildPassagePrompt(testClasses()[0]), provider.calls[1].SystemPrompt)
	// Both passes read the same words. Pass 2 is not given pass 1's answer in
	// the transcript, only in its roster.
	assert.Equal(t, provider.calls[0].UserPrompt, provider.calls[1].UserPrompt)
}

// TestExtractPassagesRunsPass2Alone covers the entry point a caller uses when
// it already knows the class — the teacher picking one on the done card
// (#127). No class question is asked, and the guard still runs.
func TestExtractPassagesRunsPass2Alone(t *testing.T) {
	provider := &twoPassProvider{replies: []string{
		`{"observations":[
			{"kind":"child","spoken_labels":["Colm"],"student":"Côme","summary":"read well"},
			{"kind":"child","spoken_labels":["She"],"student":"Maxence","summary":"she got on with it"}
		]}`,
	}}
	class := testClasses()[0]

	got, err := newLLMExtractor(provider).ExtractPassages(t.Context(), "Colm read well. She got on with it.", class)
	require.NoError(t, err)

	assert.Equal(t, []ExtractedPassage{
		{Kind: PassageChild, SpokenLabels: []string{"Colm"}, Student: "Côme", Summary: "read well"},
		{Kind: PassageUnknown, Summary: "she got on with it"},
	}, got, "the guard must run here too, not only inside Extract")

	require.Len(t, provider.calls, 1, "pass 1 must not run: the caller already named the class")
	assert.Equal(t, BuildPassagePrompt(class), provider.calls[0].SystemPrompt)
	assert.Equal(t, "Colm read well. She got on with it.", provider.calls[0].UserPrompt)
	assert.Equal(t, "passages", provider.calls[0].SchemaName)
	assert.JSONEq(t, string(passageSchema(class)), string(provider.calls[0].Schema))
}

// TestExtractWithNoRosterSkipsTheModel: the caller tolerates a failed roster
// read (voice_note_process.go logs and continues), and there is no class to
// pin, no child to reach, and no enum a provider would accept.
func TestExtractWithNoRosterSkipsTheModel(t *testing.T) {
	provider := &twoPassProvider{}

	got, err := newLLMExtractor(provider).Extract(t.Context(), ExtractRequest{Transcript: "words"})
	require.NoError(t, err)

	assert.Empty(t, got.ClassName)
	assert.Empty(t, got.Passages)
	assert.Empty(t, provider.calls, "no roster means nothing to ask")
}

// TestExtractDeclines: the header was missing, or it named two classes, and
// pass 1 said so rather than guessing. No pass 2 — there is no roster to run it
// against — and no error: a decline is a finished recording with no notes, not
// a failed one. The empty class name is what the pipeline reads to put the
// class picker on the card.
func TestExtractDeclines(t *testing.T) {
	provider := &twoPassProvider{replies: []string{`{"class_name":""}`}}

	got, err := newLLMExtractor(provider).Extract(t.Context(), ExtractRequest{
		Transcript: "No header at all. Colm read well.",
		Classes:    testClasses(),
	})
	require.NoError(t, err, "a decline must not fail the job: a failed card offers retry, not a class")

	assert.Empty(t, got.ClassName)
	assert.Empty(t, got.Passages)
	assert.Len(t, provider.calls, 1, "pass 2 has no roster to run against")
}

// TestExtractRejectsAClassOffTheRoster: pass 1's schema is a strict enum over
// the teacher's own class names plus "", so any other value means a provider
// ignoring the schema. Failing the job says so; returning no passages would
// call the recording empty — and is indistinguishable from the decline above.
func TestExtractRejectsAClassOffTheRoster(t *testing.T) {
	provider := &twoPassProvider{replies: []string{`{"class_name":"Period 9"}`}}

	_, err := newLLMExtractor(provider).Extract(t.Context(), ExtractRequest{
		Transcript: "words",
		Classes:    testClasses(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Period 9")
	assert.Len(t, provider.calls, 1, "pass 2 must not run without a class to scope it to")
}

// TestExtractNamesTheFailingPass: two calls means two ways to fail, and the
// job's error is the only thing that says which.
func TestExtractNamesTheFailingPass(t *testing.T) {
	t.Run("pass 1", func(t *testing.T) {
		provider := &twoPassProvider{errs: []error{errors.New("boom")}}
		_, err := newLLMExtractor(provider).Extract(t.Context(), ExtractRequest{Transcript: "w", Classes: testClasses()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pass 1")
	})

	t.Run("pass 2", func(t *testing.T) {
		provider := &twoPassProvider{
			replies: []string{`{"class_name":"Period 3"}`, ""},
			errs:    []error{nil, errors.New("boom")},
		}
		_, err := newLLMExtractor(provider).Extract(t.Context(), ExtractRequest{Transcript: "w", Classes: testClasses()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pass 2")
	})
}
