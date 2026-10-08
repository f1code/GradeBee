// evals_config_test.go guards the eval harness against grading a model that
// production does not run.
package handler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// promptfooConfig is the minimal slice of a promptfoo config file needed to
// check which model a provider grades.
type promptfooConfig struct {
	Providers []struct {
		ID    string `yaml:"id"`
		Label string `yaml:"label"`
	} `yaml:"providers"`
}

// TestEvalConfigsTrackProductionModels asserts that each eval config's canonical
// provider — the one evals/scripts/diff-baseline.js scores for regressions —
// names the same provider and model as the deployed configuration:
// extraction on Mistral, reports on OpenRouter (ADR 0006).
//
// Without this the two drift silently: the extraction config spent its life
// pinned to mistral-small-2603 while production ran mistral-medium-2508, so
// every pinned eval baseline score described a model we do not ship.
//
// Scope, deliberately narrower than LoadProvider: the deployed provider per
// task lives in env (LLM_PROVIDER, LLM_PROVIDER_REPORT), which this test cannot
// read, so it is written down in the cases below. It does not follow the
// LLM_MODEL_* overrides resolveModels() applies either.
//
// Non-canonical providers are deliberately unchecked. They exist to compare
// other models and are free to name anything.
func TestEvalConfigsTrackProductionModels(t *testing.T) {
	cases := []struct {
		file     string
		label    string
		provider string
		task     LLMTask
	}{
		{"promptfooconfig.extract.yaml", "gradebee-extract", "mistral", LLMTaskExtraction},
		{"promptfooconfig.report.yaml", "gradebee-report", "openrouter", LLMTaskReport},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("evals", tc.file))
			require.NoError(t, err)

			var cfg promptfooConfig
			require.NoError(t, yaml.Unmarshal(raw, &cfg))

			var got string
			for _, p := range cfg.Providers {
				if p.Label != tc.label {
					continue
				}
				require.Empty(t, got, "more than one provider is labelled %q", tc.label)
				got = p.ID
			}
			require.NotEmpty(t, got, "no provider labelled %q in %s", tc.label, tc.file)

			assert.Equal(t, tc.provider+":"+defaultModels(tc.provider)[tc.task], got,
				"%s grades a different model than defaultModels(%q): fix the "+
					"provider id, or update defaultModels() and run make eval-baseline",
				tc.file, tc.provider)
		})
	}
}
