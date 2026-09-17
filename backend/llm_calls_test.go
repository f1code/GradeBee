// llm_calls_test.go covers the llm_calls rows instrumentProvider writes and
// scripts/ai_cost.sql, which reads them.
package handler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type llmCallRow struct {
	UserID, Provider, Model, Task        string
	TraceID                              sql.NullString
	InputTokens, OutputTokens, AudioSecs sql.NullInt64
	OK                                   bool
}

func llmCallRows(t *testing.T, db *sql.DB) []llmCallRow {
	t.Helper()
	rows, err := db.Query(`SELECT user_id, trace_id, provider, model, task, input_tokens, output_tokens, audio_seconds, ok FROM llm_calls ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []llmCallRow
	for rows.Next() {
		var r llmCallRow
		require.NoError(t, rows.Scan(&r.UserID, &r.TraceID, &r.Provider, &r.Model, &r.Task, &r.InputTokens, &r.OutputTokens, &r.AudioSecs, &r.OK))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func llmCallsFake(resp LLMResponse, err error) *metricsFakeProvider {
	return &metricsFakeProvider{
		name: "mistral",
		models: map[LLMTask]string{
			LLMTaskExtraction:    "mistral-medium-2508",
			LLMTaskTranscription: "voxtral-mini-latest",
		},
		resp:        resp,
		chatJSONErr: err,
	}
}

func TestInstrumentedProvider_RecordsChatTokens(t *testing.T) {
	db := setupTestDB(t)
	p := instrumentProvider(llmCallsFake(LLMResponse{Text: "{}", Usage: &LLMUsage{InputTokens: 120, OutputTokens: 30}}, nil), db)

	_, err := p.ChatJSON(withLLMCaller(context.Background(), "user_1", "trace-1"), ChatJSONRequest{}, &struct{}{})
	require.NoError(t, err)

	assert.Equal(t, []llmCallRow{{
		UserID: "user_1", TraceID: sql.NullString{String: "trace-1", Valid: true},
		Provider: "mistral", Model: "mistral-medium-2508", Task: "extraction",
		InputTokens: sql.NullInt64{Int64: 120, Valid: true}, OutputTokens: sql.NullInt64{Int64: 30, Valid: true},
		OK: true,
	}}, llmCallRows(t, db))
}

func TestInstrumentedProvider_RecordsTranscriptionSeconds(t *testing.T) {
	db := setupTestDB(t)
	p := instrumentProvider(llmCallsFake(LLMResponse{Text: "hi", Usage: &LLMUsage{AudioSeconds: 42}}, nil), db)

	_, err := p.Transcribe(withLLMCaller(context.Background(), "user_1", ""), TranscribeRequest{})
	require.NoError(t, err)

	assert.Equal(t, []llmCallRow{{
		UserID: "user_1", Provider: "mistral", Model: "voxtral-mini-latest", Task: "transcription",
		AudioSecs: sql.NullInt64{Int64: 42, Valid: true}, OK: true,
	}}, llmCallRows(t, db))
}

func TestInstrumentedProvider_DecodeFailureRecordsNotOK(t *testing.T) {
	db := setupTestDB(t)
	decodeErr := errors.New("failed to parse extraction response")
	p := instrumentProvider(llmCallsFake(LLMResponse{Usage: &LLMUsage{InputTokens: 5, OutputTokens: 1}}, decodeErr), db)

	_, err := p.ChatJSON(withLLMCaller(context.Background(), "user_1", ""), ChatJSONRequest{}, &struct{}{})
	require.ErrorIs(t, err, decodeErr)

	rows := llmCallRows(t, db)
	require.Len(t, rows, 1)
	assert.False(t, rows[0].OK)
	assert.Equal(t, int64(5), rows[0].InputTokens.Int64)
}

func TestInstrumentedProvider_NoResponseNoRow(t *testing.T) {
	db := setupTestDB(t)
	p := instrumentProvider(llmCallsFake(LLMResponse{}, context.DeadlineExceeded), db)

	_, err := p.ChatJSON(withLLMCaller(context.Background(), "user_1", ""), ChatJSONRequest{}, &struct{}{})
	require.Error(t, err)
	assert.Empty(t, llmCallRows(t, db))
}

func TestInstrumentedProvider_MissingUserStillReturnsResult(t *testing.T) {
	db := setupTestDB(t)
	p := instrumentProvider(llmCallsFake(LLMResponse{Text: "ok", Usage: &LLMUsage{InputTokens: 1}}, nil), db)

	resp, err := p.ChatJSON(context.Background(), ChatJSONRequest{}, &struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Text)
	assert.Empty(t, llmCallRows(t, db))
}

func TestInstrumentedProvider_InsertFailureStillReturnsResult(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.Close())
	p := instrumentProvider(llmCallsFake(LLMResponse{Text: "ok", Usage: &LLMUsage{InputTokens: 1}}, nil), db)

	resp, err := p.ChatJSON(withLLMCaller(context.Background(), "user_1", ""), ChatJSONRequest{}, &struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Text)
}

func TestAICostScript(t *testing.T) {
	db := setupTestDB(t)
	script, err := os.ReadFile("../scripts/ai_cost.sql")
	require.NoError(t, err)

	yesterday := `strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 day')`
	_, err = db.Exec(`INSERT INTO llm_calls (created_at, user_id, provider, model, task, input_tokens, output_tokens, audio_seconds, ok) VALUES
		(` + yesterday + `, 'priced', 'mistral', 'mistral-medium-2508', 'extraction', 1000000, 1000000, NULL, 1),
		(` + yesterday + `, 'priced', 'mistral', 'voxtral-mini-latest', 'transcription', NULL, NULL, 120, 1),
		(` + yesterday + `, 'unpriced', 'mistral', 'no-such-model', 'report', 500, 50, NULL, 0),
		(strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), 'priced', 'mistral', 'mistral-medium-2508', 'report', 1000000, 0, NULL, 1)`)
	require.NoError(t, err)

	rows, err := db.Query(string(script))
	require.NoError(t, err)
	defer rows.Close()
	type costRow struct {
		UserID                           string
		Calls, InputTokens, OutputTokens int64
		AudioMinutes, USD                float64
		UnpricedCalls                    int64
	}
	var got []costRow
	for rows.Next() {
		var r costRow
		require.NoError(t, rows.Scan(&r.UserID, &r.Calls, &r.InputTokens, &r.OutputTokens, &r.AudioMinutes, &r.USD, &r.UnpricedCalls))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())

	// priced: $0.40 + $2.00 for the tokens, 2 min x $0.003 for the audio. Today's row is out of range.
	assert.Equal(t, []costRow{
		{UserID: "priced", Calls: 2, InputTokens: 1000000, OutputTokens: 1000000, AudioMinutes: 2, USD: 2.406},
		{UserID: "unpriced", Calls: 1, InputTokens: 500, OutputTokens: 50, UnpricedCalls: 1},
	}, got)
}
