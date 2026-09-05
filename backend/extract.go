// extract.go defines the Extractor interface and its LLM implementation.
//
// Extraction is two model calls (#125).
//
// Pass 1 is shown the teacher's class list and nothing else, and names the
// class the recording is about. Pass 2 is shown that one class's children and
// cuts the transcript into passages: who each stretch of speech is about, the
// names the teacher actually spoke for them, and a summary in the teacher's
// own voice.
//
// Scoping the roster to one class is the point. A roster spanning every class
// puts dozens of names in front of the model, and first names repeat across
// classes; measured, the whole roster is also what makes the model file an
// unnamed "she" block under a listed child (#99). One class's names are enough
// to spell a garbled name correctly and few enough that the prompt's
// no-elimination rules hold — 0/10 phantoms against 8/10 without them
// (research/2026-09-05-123-summaries-vs-spans, arm V1).
//
// Name resolution stays with the model here: it sees the transcript, and on a
// small roster it beats MatchStudent on garbled names (70 agreements, 0
// disagreements, 7 the model alone resolved). MatchStudent is the second pass
// for a recording read against the wrong class — see voice_note_assemble.go.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Extractor takes a transcript + student roster and returns structured extraction.
type Extractor interface {
	// Extract runs both passes: the class, then the passages against it.
	Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error)
	// ExtractPassages runs pass 2 alone, against a class the caller already
	// knows. It is what a teacher picking the class on the done card needs:
	// the recording is re-read against their answer, with no class question to
	// ask. Extract calls it after pass 1, so there is one pass 2, not two.
	ExtractPassages(ctx context.Context, transcript string, class ClassGroup) ([]ExtractedPassage, error)
	// Model returns the model ID used for extraction (for stamping model_version).
	Model() string
}

// ExtractRequest is the input to an extraction call.
type ExtractRequest struct {
	Transcript string
	Classes    []ClassGroup
}

// ExtractResponse is the structured output from extraction: the class pass 1
// pinned, and the passages pass 2 cut the transcript into.
type ExtractResponse struct {
	ClassName string
	Passages  []ExtractedPassage
}

// ExtractedPassage is one stretch of the recording as pass 2 read it.
//
// The JSON tags are the model's contract, and the field order is its
// generation order: kind first, then the names as spoken, then the child they
// resolve to, then the summary. Reaching student before spoken_labels is what
// lets the model pick a child and invent a label to justify it.
type ExtractedPassage struct {
	Kind PassageKind `json:"kind"`
	// SpokenLabels is each name the teacher spoke for this passage, verbatim
	// and uncorrected. Empty for every kind but child. It is what the class
	// picker re-resolves when a recording was read against the wrong roster,
	// and what the pronoun guard reads.
	SpokenLabels []string `json:"spoken_labels"`
	// Student is the roster name this passage reached, or "" when no child on
	// the pinned class's roster fits. The schema constrains it to that roster,
	// so a non-empty value is always a real child of the pinned class.
	Student string `json:"student"`
	// Summary is the passage rewritten as clear sentences in the teacher's
	// voice. It is what a note built from this passage holds.
	Summary string `json:"summary"`
}

// llmExtractor uses an LLMProvider to extract student mentions from transcripts.
type llmExtractor struct {
	provider LLMProvider
}

func newLLMExtractor(provider LLMProvider) *llmExtractor {
	return &llmExtractor{provider: provider}
}

func (e *llmExtractor) Model() string {
	return e.provider.Model(LLMTaskExtraction)
}

// Extract runs both passes and returns the guarded passages.
//
// A roster read that returned nothing short-circuits: there is no class to pin
// and no child to reach, and an enum of no values is not a schema the provider
// will accept. The caller tolerates a failed roster read (voice_note_process.go
// logs and continues), so this is a real path, not a defensive one.
func (e *llmExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	if len(req.Classes) == 0 {
		return &ExtractResponse{}, nil
	}

	var pass1 struct {
		ClassName string `json:"class_name"`
	}
	if _, err := e.provider.ChatJSON(ctx, ChatJSONRequest{
		SystemPrompt: BuildClassPickPrompt(req.Classes),
		UserPrompt:   req.Transcript,
		SchemaName:   "class_pick",
		Schema:       classPickSchema(req.Classes),
	}, &pass1); err != nil {
		return nil, fmt.Errorf("extraction pass 1 failed: %w", err)
	}

	class, ok := findClass(req.Classes, pass1.ClassName)
	if !ok {
		// The pass-1 schema is a strict enum over these very names, so this is
		// a provider that ignored it. Failing loudly beats returning no notes
		// and calling the recording empty.
		return nil, fmt.Errorf("extraction pass 1 returned class %q, which is not on the roster", pass1.ClassName)
	}

	passages, err := e.ExtractPassages(ctx, req.Transcript, class)
	if err != nil {
		return nil, err
	}
	return &ExtractResponse{ClassName: class.Name, Passages: passages}, nil
}

// ExtractPassages runs pass 2 against one class and guards what comes back.
func (e *llmExtractor) ExtractPassages(ctx context.Context, transcript string, class ClassGroup) ([]ExtractedPassage, error) {
	var pass2 struct {
		Observations []ExtractedPassage `json:"observations"`
	}
	if _, err := e.provider.ChatJSON(ctx, ChatJSONRequest{
		SystemPrompt: BuildPassagePrompt(class),
		UserPrompt:   transcript,
		SchemaName:   "passages",
		Schema:       passageSchema(class),
	}, &pass2); err != nil {
		return nil, fmt.Errorf("extraction pass 2 failed: %w", err)
	}
	return guardPassages(pass2.Observations), nil
}

// BuildClassPickPrompt builds pass 1's system prompt: the class names, and
// nothing about the children in them.
func BuildClassPickPrompt(classes []ClassGroup) string {
	var sb strings.Builder
	sb.WriteString(classPickPrompt)
	for _, c := range classes {
		sb.WriteString("- " + c.Name + "\n")
	}
	sb.WriteString(classPickPromptSuffix)
	return sb.String()
}

// BuildPassagePrompt builds pass 2's system prompt: the rules, then one
// class's children.
//
// Aliases read as "also called", not "aka": the list is there to spell a
// spoken name correctly, and an alias is another thing the teacher says out
// loud, not a second identity.
func BuildPassagePrompt(class ClassGroup) string {
	var sb strings.Builder
	sb.WriteString(passagePromptPrefix)
	for _, s := range class.Students {
		if len(s.Aliases) > 0 {
			sb.WriteString(fmt.Sprintf("- %s (also called %s)\n", s.Name, strings.Join(s.Aliases, ", ")))
		} else {
			sb.WriteString("- " + s.Name + "\n")
		}
	}
	return sb.String()
}

// guardPassages demotes a child passage the model attributed with no spoken
// name to unknown, so its summary reaches the unattributed list instead of a
// child's note.
//
// This is the structural backstop under the prompt's no-elimination rules. In
// every roster phantom measured across 280 runs — 15 of them, on every prompt
// variant — the model had labelled the block "She", so dropping a child
// passage whose labels are all pronouns removed 100% of them with no false
// positive. A passage carrying one real name passes untouched: a mixed list
// ("She", "Ombeline") is a named passage.
//
// The stop list is match.go's, so the guard and the matcher agree on what is
// never a name.
func guardPassages(passages []ExtractedPassage) []ExtractedPassage {
	out := make([]ExtractedPassage, len(passages))
	for i, p := range passages {
		if p.Kind == PassageChild && !hasSpokenName(p.SpokenLabels) {
			p.Kind = PassageUnknown
			p.SpokenLabels = nil
			p.Student = ""
		}
		out[i] = p
	}
	return out
}

// hasSpokenName reports whether any label could be a name at all.
func hasSpokenName(labels []string) bool {
	for _, l := range labels {
		key := foldName(l)
		if key != "" && !labelStopList[key] {
			return true
		}
	}
	return false
}

// findClass returns the class with this exact name. Class names are compared
// by exact string everywhere on this API.
func findClass(classes []ClassGroup, name string) (ClassGroup, bool) {
	for _, c := range classes {
		if c.Name == name {
			return c, true
		}
	}
	return ClassGroup{}, false
}

// classPickSchema is pass 1's schema: one field, constrained to the teacher's
// own class names.
//
// The enum does not carry "", so the model cannot decline and always pins a
// class — even though classPickPromptSuffix tells it to return "" when no
// class is identifiable. That inert instruction is deliberate: #127 turns the
// decline on by adding "" here, and everything else about both passes stays
// as it is.
func classPickSchema(classes []ClassGroup) json.RawMessage {
	names := make([]string, 0, len(classes))
	for _, c := range classes {
		names = append(names, c.Name)
	}
	return jsonObject(
		field("type", "object"),
		field("properties", map[string]any{
			"class_name": map[string]any{"type": "string", "enum": names},
		}),
		field("required", []string{"class_name"}),
		field("additionalProperties", false),
	)
}

// PassageSchema is pass 2's schema. student is an enum of one class's roster
// plus "", so a named passage always reaches a real child of the pinned class
// and there is a value for "nobody here fits".
//
// Exported for the eval harness: promptfoo's provider config is static and
// cannot template a per-fixture roster, so eval-cli writes these out per
// fixture (cmd/eval-cli, schema mode) rather than the config restating the
// contract in YAML.
//
// The properties are emitted in declared order, not built from a map, because
// under structured output the schema's property order is the model's
// generation order — and the whole point of putting spoken_labels before
// student is that the model writes down what it heard before it decides who
// that was. encoding/json sorts map keys; these four names happen to sort into
// the right order today, which would hide the bug until one was renamed.
func passageSchema(class ClassGroup) json.RawMessage {
	names := make([]string, 0, len(class.Students)+1)
	for _, s := range class.Students {
		names = append(names, s.Name)
	}
	names = append(names, "")

	passage := jsonObject(
		field("type", "object"),
		field("properties", jsonObject(
			field("kind", map[string]any{
				"type": "string",
				"enum": []PassageKind{PassageChild, PassageUnknown, PassageGroup, PassageNone},
			}),
			field("spoken_labels", map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			}),
			field("student", map[string]any{"type": "string", "enum": names}),
			field("summary", map[string]any{"type": "string"}),
		)),
		field("required", []string{"kind", "spoken_labels", "student", "summary"}),
		field("additionalProperties", false),
	)

	return jsonObject(
		field("type", "object"),
		field("properties", jsonObject(
			field("observations", map[string]any{"type": "array", "items": passage}),
		)),
		field("required", []string{"observations"}),
		field("additionalProperties", false),
	)
}

// jsonField is one key of an object literal, in the order it was declared.
type jsonField struct {
	key   string
	value any
}

func field(key string, value any) jsonField { return jsonField{key: key, value: value} }

// jsonObject marshals fields into a JSON object that keeps their declared
// order. A json.RawMessage value is written through unchanged, so objects
// nest.
func jsonObject(fields ...jsonField) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(f.key)
		if err != nil {
			panic(fmt.Sprintf("jsonObject: key %q: %v", f.key, err))
		}
		value, err := json.Marshal(f.value)
		if err != nil {
			// Every value here is a static literal built in this file, so a
			// marshal error is a bug in the schema, not a runtime condition.
			panic(fmt.Sprintf("jsonObject: value for %q: %v", f.key, err))
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return json.RawMessage(b.Bytes())
}
