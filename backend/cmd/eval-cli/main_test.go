package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
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

// captureOutput redirects os.Stdout for the duration of f() and returns what was written.
// Not goroutine-safe — use only in sequential tests.
func captureOutput(f func() error) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("captureOutput: pipe: %v", err))
	}
	old := os.Stdout
	os.Stdout = w
	runErr := f()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		panic(fmt.Sprintf("captureOutput: copy: %v", err))
	}
	return buf.String(), runErr
}

func TestRun_MissingArgs(t *testing.T) {
	err := run([]string{"eval-cli"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRun_NonJSONArg(t *testing.T) {
	err := run([]string{"eval-cli", "unknown"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunPromptMode_UnknownTask(t *testing.T) {
	err := runPromptMode(`{"vars":{},"config":{"task":"bogus"}}`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown task")
}

func TestRunBuildExtractPrompt(t *testing.T) {
	ec := evalContext{Vars: mustRawMap(map[string]interface{}{
		"transcript": "Alice read well today.",
		"class_name": "Grade 3A",
		"classes": []interface{}{
			map[string]interface{}{"name": "Grade 3A", "students": []interface{}{
				map[string]interface{}{"name": "Alice Chen"},
			}},
			map[string]interface{}{"name": "Grade 3B", "students": []interface{}{
				map[string]interface{}{"name": "Bob Ruiz"},
			}},
		},
	})}
	out, err := captureOutput(func() error { return runBuildExtractPrompt(ec) })
	require.NoError(t, err)
	var msgs []map[string]string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &msgs))
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0]["role"])
	// The roster in the prompt is the named class alone. A prompt carrying
	// every class would grade a contract production does not run.
	assert.Contains(t, msgs[0]["content"], "Alice Chen")
	assert.NotContains(t, msgs[0]["content"], "Bob Ruiz")
	assert.Equal(t, "user", msgs[1]["role"])
	assert.Equal(t, "Alice read well today.", msgs[1]["content"])
}

// The class is the fixture's answer for what pass 1 pins, so a missing or
// unknown one is a broken test row, not a prompt to build anyway.
func TestRunBuildExtractPrompt_ClassNameRequired(t *testing.T) {
	classes := []interface{}{
		map[string]interface{}{"name": "Grade 3A", "students": []interface{}{}},
	}

	t.Run("missing", func(t *testing.T) {
		ec := evalContext{Vars: mustRawMap(map[string]interface{}{
			"transcript": "Alice read well today.",
			"classes":    classes,
		})}
		err := runBuildExtractPrompt(ec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "class_name is required")
	})

	t.Run("not one of the fixture's classes", func(t *testing.T) {
		ec := evalContext{Vars: mustRawMap(map[string]interface{}{
			"transcript": "Alice read well today.",
			"class_name": "Grade 9Z",
			"classes":    classes,
		})}
		err := runBuildExtractPrompt(ec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Grade 9Z")
	})
}

func TestRunBuildExtractPrompt_MissingTranscript(t *testing.T) {
	ec := evalContext{Vars: mustRawMap(map[string]interface{}{
		"transcript": "",
		"classes":    []interface{}{},
	})}
	_, err := captureOutput(func() error { return runBuildExtractPrompt(ec) })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transcript")
}

func TestRunBuildReportPrompt(t *testing.T) {
	ec := evalContext{Vars: mustRawMap(map[string]interface{}{
		"student_name":        "Alice",
		"class_name":          "Grade 3A",
		"notes":               []interface{}{map[string]interface{}{"date": "2026-01-15", "summary": "Strong reader."}},
		"examples":            []interface{}{},
		"report_instructions": "Write a three-section report.",
		"instructions":        "",
	})}
	out, err := captureOutput(func() error { return runBuildReportPrompt(ec) })
	require.NoError(t, err)
	var msgs []map[string]string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &msgs))
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0]["role"])
	assert.NotEmpty(t, msgs[0]["content"])
}
