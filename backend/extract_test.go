package handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Live extraction tests (those that call a real LLM) live in llm_live_test.go
// behind the `llm` build tag; this file holds only offline checks.

// schemaProp mirrors the subset of JSON Schema shape produced by
// extractResponseSchema, enough to inspect the class_name enum constraint.
type schemaProp struct {
	Type       string                `json:"type"`
	Enum       []string              `json:"enum,omitempty"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
	Items      *schemaProp           `json:"items,omitempty"`
}

// TestExtractResponseSchemaClassNameEnum verifies that extractResponseSchema
// constrains class_name to an enum of the roster's actual class names, for
// both students[] and students[].candidates[], and that an empty roster
// falls back to a plain string instead of an unsatisfiable empty enum.
func TestExtractResponseSchemaClassNameEnum(t *testing.T) {
	classes := []ClassGroup{
		{Name: "Period 3", Students: []ClassStudent{{Name: "Maxence"}}},
		{Name: "Period 5", Students: []ClassStudent{{Name: "Amara"}}},
	}

	var schema schemaProp
	require.NoError(t, json.Unmarshal(extractResponseSchema(classes), &schema))

	studentItems := schema.Properties["students"].Items
	require.NotNil(t, studentItems)
	assert.ElementsMatch(t, []string{"Period 3", "Period 5"}, studentItems.Properties["class_name"].Enum)

	candidateItems := studentItems.Properties["candidates"].Items
	require.NotNil(t, candidateItems)
	assert.ElementsMatch(t, []string{"Period 3", "Period 5"}, candidateItems.Properties["class_name"].Enum)

	// Empty roster: no enum, plain string type.
	var emptySchema schemaProp
	require.NoError(t, json.Unmarshal(extractResponseSchema(nil), &emptySchema))
	emptyStudentItems := emptySchema.Properties["students"].Items
	require.NotNil(t, emptyStudentItems)
	emptyClassName := emptyStudentItems.Properties["class_name"]
	assert.Empty(t, emptyClassName.Enum)
	assert.Equal(t, "string", emptyClassName.Type)
}
