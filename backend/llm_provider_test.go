package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, "mistral-medium-3-5", mistralModels[LLMTaskExtraction])
	assert.Equal(t, "mistral-medium-3-5", mistralModels[LLMTaskReport])
	assert.Equal(t, "voxtral-mini-latest", mistralModels[LLMTaskTranscription])

	openaiModels := defaultModels("openai")
	assert.Equal(t, "gpt-5.4-mini", openaiModels[LLMTaskExtraction])
	assert.Equal(t, "gpt-5.4-mini", openaiModels[LLMTaskReport])
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

func newTestMistralProvider(url string) *mistralProvider {
	return newMistralProvider("key", url+"/v1", map[LLMTask]string{LLMTaskTranscription: "voxtral-mini-latest"})
}

func TestMistralTranscribe_SendsMultipartAndDecodesUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		assert.Equal(t, "Bearer key", r.Header.Get("Authorization"))
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "voxtral-mini-latest", r.FormValue("model"))
		assert.Equal(t, []string{"Grade_6", "Math"}, r.MultipartForm.Value["context_bias[]"])
		f, hdr, err := r.FormFile("file")
		require.NoError(t, err)
		audio, _ := io.ReadAll(f) //nolint:errcheck // test server
		assert.Equal(t, "note.mp3", hdr.Filename)
		assert.Equal(t, "AUDIO", string(audio))
		_, _ = io.WriteString(w, `{"text":"hello","usage":{"prompt_audio_seconds":42,"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`) //nolint:errcheck // test server
	}))
	defer srv.Close()

	resp, err := newTestMistralProvider(srv.URL).Transcribe(context.Background(), TranscribeRequest{
		Filename:    "dir/note.mp3",
		Audio:       strings.NewReader("AUDIO"),
		ContextBias: []string{"Grade 6", "Math", "math"},
	})
	require.NoError(t, err)
	assert.Equal(t, LLMResponse{Text: "hello", Usage: &LLMUsage{AudioSeconds: 42}}, resp)
}

func TestMistralTranscribe_Non2xxReturnsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := newTestMistralProvider(srv.URL).Transcribe(context.Background(), TranscribeRequest{Audio: strings.NewReader("x")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "quota exceeded")
}

func TestMistralTranscribe_CtxCancelAbortsRequest(t *testing.T) {
	received := make(chan struct{})
	aborted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server notices client disconnect only once the body is drained.
		_, _ = io.Copy(io.Discard, r.Body) //nolint:errcheck // test server
		close(received)
		select {
		case <-r.Context().Done():
			close(aborted)
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-received
		cancel()
	}()
	_, err := newTestMistralProvider(srv.URL).Transcribe(ctx, TranscribeRequest{Audio: strings.NewReader("x")})
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	select {
	case <-aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("server request not aborted")
	}
}

func TestMistralChatJSON_DecodeFailureReturnsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"not json"}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`) //nolint:errcheck // test server
	}))
	defer srv.Close()

	resp, err := newTestMistralProvider(srv.URL).ChatJSON(context.Background(), ChatJSONRequest{}, &struct{}{})
	require.Error(t, err)
	assert.Equal(t, &LLMUsage{InputTokens: 7, OutputTokens: 3}, resp.Usage)
}

func TestMistralTranscribe_DecodeFailureReturnsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not json`) //nolint:errcheck // test server
	}))
	defer srv.Close()

	resp, err := newTestMistralProvider(srv.URL).Transcribe(context.Background(), TranscribeRequest{Audio: strings.NewReader("x")})
	require.Error(t, err)
	assert.NotNil(t, resp.Usage)
}

func TestOpenAITranscribe_RoundsDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "verbose_json", r.FormValue("response_format"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hi","duration":41.6}`) //nolint:errcheck // test server
	}))
	defer srv.Close()

	p := newOpenAIProvider("key", srv.URL+"/v1", map[LLMTask]string{LLMTaskTranscription: "whisper-1"})
	resp, err := p.Transcribe(context.Background(), TranscribeRequest{Filename: "a.mp3", Audio: strings.NewReader("x")})
	require.NoError(t, err)
	assert.Equal(t, LLMResponse{Text: "hi", Usage: &LLMUsage{AudioSeconds: 42}}, resp)
}

// clearLLMEnv unsets every variable LoadProvider reads, so a developer's
// local env does not leak into the test.
func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LLM_PROVIDER", "LLM_PROVIDER_EXTRACTION", "LLM_PROVIDER_REPORT", "LLM_PROVIDER_TRANSCRIPTION",
		"LLM_MODEL_EXTRACTION", "LLM_MODEL_REPORT", "LLM_MODEL_TRANSCRIPTION",
		"OPENAI_API_KEY", "MISTRAL_API_KEY", "OPENROUTER_API_KEY",
		"OPENAI_BASE_URL", "MISTRAL_BASE_URL", "OPENROUTER_BASE_URL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadProvider_PerTask(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		check   func(t *testing.T, p LLMProvider)
	}{
		{
			name: "all tasks on one provider skip the router",
			env:  map[string]string{"LLM_PROVIDER": "mistral", "MISTRAL_API_KEY": "k"},
			check: func(t *testing.T, p LLMProvider) {
				_, ok := p.(*instrumentedProvider)
				assert.True(t, ok, "got %T", p)
				assert.Equal(t, "mistral", p.Name())
			},
		},
		{
			name: "report on openrouter",
			env:  map[string]string{"LLM_PROVIDER": "mistral", "LLM_PROVIDER_REPORT": "openrouter", "MISTRAL_API_KEY": "k", "OPENROUTER_API_KEY": "k"},
			check: func(t *testing.T, p LLMProvider) {
				r, ok := p.(*taskRouter)
				require.True(t, ok, "got %T", p)
				assert.Equal(t, "openrouter", r.report.Name())
				assert.Equal(t, "mistral", r.extraction.Name())
				assert.Equal(t, "mistral", r.transcription.Name())
				assert.Equal(t, "openai/gpt-6-luna", p.Model(LLMTaskReport))
				assert.Equal(t, "mistral-medium-3-5", p.Model(LLMTaskExtraction))
				assert.Equal(t, "voxtral-mini-latest", p.Model(LLMTaskTranscription))
			},
		},
		{
			name:    "unknown task provider names its variable",
			env:     map[string]string{"LLM_PROVIDER": "mistral", "LLM_PROVIDER_REPORT": "anthropic", "MISTRAL_API_KEY": "k"},
			wantErr: `unknown LLM_PROVIDER_REPORT "anthropic"`,
		},
		{
			name:    "unknown default provider",
			env:     map[string]string{"LLM_PROVIDER": "gemini"},
			wantErr: `unknown LLM_PROVIDER "gemini"`,
		},
		{
			name:    "missing key for the report provider",
			env:     map[string]string{"LLM_PROVIDER": "mistral", "LLM_PROVIDER_REPORT": "openrouter", "MISTRAL_API_KEY": "k"},
			wantErr: "OPENROUTER_API_KEY is not set",
		},
		{
			name:    "missing key for the default provider",
			env:     map[string]string{"LLM_PROVIDER": "mistral", "LLM_PROVIDER_REPORT": "openrouter", "OPENROUTER_API_KEY": "k"},
			wantErr: "MISTRAL_API_KEY is not set",
		},
		{
			name:    "openrouter refused for transcription",
			env:     map[string]string{"LLM_PROVIDER": "openrouter", "OPENROUTER_API_KEY": "k"},
			wantErr: "transcription cannot use openrouter",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearLLMEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			p, err := LoadProvider(nil)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.check(t, p)
		})
	}
}

func TestOpenRouterBaseURL_DefaultsToEU(t *testing.T) {
	t.Setenv("OPENROUTER_BASE_URL", "")
	assert.Equal(t, "https://eu.openrouter.ai/api/v1", openRouterBaseURL())
}

// chatServer answers chat completions and Voxtral transcriptions, and records
// that it was called.
func chatServer(t *testing.T, content string, called *bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/audio/transcriptions" {
			_, _ = io.WriteString(w, `{"text":"t","usage":{"prompt_audio_seconds":3}}`) //nolint:errcheck // test server
			return
		}
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+content+`}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`) //nolint:errcheck // test server
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLoadProvider_RoutesTasksAndRecordsRealProvider(t *testing.T) {
	var mistralHit, openrouterHit bool
	mistralSrv := chatServer(t, `"{}"`, &mistralHit)
	openrouterSrv := chatServer(t, `"report"`, &openrouterHit)

	clearLLMEnv(t)
	t.Setenv("LLM_PROVIDER", "mistral")
	t.Setenv("LLM_PROVIDER_REPORT", "openrouter")
	t.Setenv("MISTRAL_API_KEY", "k")
	t.Setenv("OPENROUTER_API_KEY", "k")
	t.Setenv("MISTRAL_BASE_URL", mistralSrv.URL+"/v1")
	t.Setenv("OPENROUTER_BASE_URL", openrouterSrv.URL+"/v1")

	db := setupTestDB(t)
	p, err := LoadProvider(db)
	require.NoError(t, err)
	ctx := withLLMCaller(context.Background(), "user_1", "")

	resp, err := p.ChatText(ctx, ChatTextRequest{UserPrompt: "x"})
	require.NoError(t, err)
	assert.Equal(t, "report", resp.Text)
	assert.True(t, openrouterHit)
	assert.False(t, mistralHit)

	_, err = p.ChatJSON(ctx, ChatJSONRequest{}, &struct{}{})
	require.NoError(t, err)
	assert.True(t, mistralHit)

	openrouterHit = false
	_, err = p.Transcribe(ctx, TranscribeRequest{Filename: "a.mp3", Audio: strings.NewReader("x")})
	require.NoError(t, err)
	assert.False(t, openrouterHit)

	rows := llmCallRows(t, db)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"mistral", "voxtral-mini-latest", "transcription"}, []string{rows[2].Provider, rows[2].Model, rows[2].Task})
	assert.Equal(t, []string{"openrouter", "openai/gpt-6-luna", "report"}, []string{rows[0].Provider, rows[0].Model, rows[0].Task})
	assert.Equal(t, []string{"mistral", "mistral-medium-3-5", "extraction"}, []string{rows[1].Provider, rows[1].Model, rows[1].Task})
}
