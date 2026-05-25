package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustRawMap converts a map[string]interface{} to map[string]json.RawMessage.
func mustRawMap(m map[string]interface{}) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		b, err := json.Marshal(v)
		if err != nil {
			panic(fmt.Sprintf("mustRawMap: marshal %s: %v", k, err))
		}
		result[k] = b
	}
	return result
}

func TestRun_MissingArgs(t *testing.T) {
	err := run([]string{"eval-cli"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRun_UnknownSubcommand(t *testing.T) {
	ctx := evalContext{Vars: mustRawMap(map[string]interface{}{})}
	b, err2 := json.Marshal(ctx)
	require.NoError(t, err2)
	err := run([]string{"eval-cli", "unknown", "prompt", "{}", string(b)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown subcommand")
}

func TestRunExtract_MissingTranscript(t *testing.T) {
	_, err := runExtract(context.TODO(), "gpt-test", evalContext{
		Vars: mustRawMap(map[string]interface{}{
			"transcript": "",
			"classes":    []interface{}{},
		}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transcript")
}

func TestRunGenerateReport_MissingStudentName(t *testing.T) {
	_, err := runGenerateReport(context.TODO(), "gpt-test", evalContext{
		Vars: mustRawMap(map[string]interface{}{
			"student_name": "",
			"class":        "Grade 3A",
			"start_date":   "2026-01-01",
			"end_date":     "2026-03-31",
			"notes":        []interface{}{},
			"examples":     []interface{}{},
			"instructions": "",
		}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "student_name")
}
