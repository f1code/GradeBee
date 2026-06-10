package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLLMProvider_InterfaceConformance verifies that both provider implementations
// satisfy the LLMProvider interface at compile time.
func TestLLMProvider_InterfaceConformance(t *testing.T) {
	var _ LLMProvider = (*openaiProvider)(nil)
	var _ LLMProvider = (*mistralProvider)(nil)
}

// TestLLMProvider_DefaultModels verifies default model IDs for each provider.
func TestLLMProvider_DefaultModels(t *testing.T) {
	mistralModels := defaultModels("mistral")
	assert.Equal(t, "mistral-medium-2508", mistralModels[LLMTaskExtraction])
	assert.Equal(t, "mistral-medium-2508", mistralModels[LLMTaskReport])
	assert.Equal(t, "mistral-medium-2508", mistralModels[LLMTaskVision])
	assert.Equal(t, "voxtral-mini-latest", mistralModels[LLMTaskTranscription])

	openaiModels := defaultModels("openai")
	assert.Equal(t, "gpt-5.4-mini", openaiModels[LLMTaskExtraction])
	assert.Equal(t, "gpt-5.4-mini", openaiModels[LLMTaskReport])
	assert.Equal(t, "gpt-5.4-mini", openaiModels[LLMTaskVision])
	assert.Equal(t, "whisper-1", openaiModels[LLMTaskTranscription])
}

// TestSanitiseContextBias verifies the Voxtral context_bias sanitisation rules.
func TestSanitiseContextBias(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "plain terms pass through",
			input: []string{"Math101", "Science202"},
			want:  []string{"Math101", "Science202"},
		},
		{
			name:  "spaces replaced with underscores",
			input: []string{"Wed Marcia 1410", "Grade 6"},
			want:  []string{"Wed_Marcia_1410", "Grade_6"},
		},
		{
			name:  "commas dropped",
			input: []string{"Alice,Bob"},
			want:  []string{"AliceBob"},
		},
		{
			name:  "empty after sanitisation skipped",
			input: []string{" ", ",", ""},
			want:  nil,
		},
		{
			name:  "case-insensitive deduplication",
			input: []string{"Math", "math", "MATH"},
			want:  []string{"Math"},
		},
		{
			name:  "cap at 100",
			input: makeTerms(150),
			want:  makeTerms(100),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitiseContextBias(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func makeTerms(n int) []string {
	terms := make([]string, n)
	for i := range terms {
		terms[i] = "term" + string(rune('A'+i%26)) + string(rune('0'+i/26))
	}
	return terms
}
