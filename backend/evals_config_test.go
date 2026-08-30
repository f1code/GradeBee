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
// names the same model as defaultModels("mistral"), the deployed configuration.
//
// Without this the two drift silently: the extraction config spent its life
// pinned to mistral-small-2603 while production ran mistral-medium-2508, so
// every score in evals/baseline.json described a model we do not ship.
//
// Scope, deliberately narrower than LoadProvider: this checks the Mistral
// defaults only. It does not follow LLM_PROVIDER=openai or the LLM_MODEL_*
// overrides resolveModels() applies, because the configs hardcode a
// "mistral:"-prefixed provider id and cannot express either. A deployment that
// overrides those env vars is outside what this guard can see.
//
// Non-canonical providers are deliberately unchecked. They exist to compare
// other models and are free to name anything.
func TestEvalConfigsTrackProductionModels(t *testing.T) {
	models := defaultModels("mistral")

	cases := []struct {
		file  string
		label string
		task  LLMTask
	}{
		{"promptfooconfig.extract.yaml", "gradebee-extract", LLMTaskExtraction},
		{"promptfooconfig.report.yaml", "gradebee-report", LLMTaskReport},
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

			assert.Equal(t, "mistral:"+models[tc.task], got,
				"%s grades a different model than defaultModels(\"mistral\"): fix the "+
					"provider id, or update defaultModels() and regenerate evals/baseline.json",
				tc.file)
		})
	}
}
