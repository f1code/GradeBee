package handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// #127's two enum shapes, written out rather than built by classPickSchema, so
// this test keeps discriminating whichever shape the builder emits today.
// decliningEnum is pinned to the builder's current output below; pinningEnum is
// the pre-#127 shape and nothing emits it any more.
const (
	pinningEnum   = `{"type":"object","properties":{"class_name":{"enum":["SENTINEL_CLASS_A","SENTINEL_CLASS_B"],"type":"string"}},"required":["class_name"],"additionalProperties":false}`
	decliningEnum = `{"type":"object","properties":{"class_name":{"enum":["SENTINEL_CLASS_A","SENTINEL_CLASS_B",""],"type":"string"}},"required":["class_name"],"additionalProperties":false}`
)

// TestExtractionHashMovesWithSchema is the failure this task closes. #127's
// entire change was one value in pass 1's enum: adding "" so the model may
// decline a recording it cannot place. Measured on mistral-medium-2508, that
// value flipped the behaviour — without it the model pinned a class 3/3, with
// it the same prompt declined. The prompt text did not move, so a hash over
// text alone stamped both contracts with one value.
func TestExtractionHashMovesWithSchema(t *testing.T) {
	passage := passageSchema(sentinelClasses[0])

	// Bytes, not JSONEq: the bytes are what is hashed, and this is what keeps
	// the literals above honest as the builder changes.
	assert.Equal(t, decliningEnum, string(classPickSchema(sentinelClasses)),
		"decliningEnum no longer matches what classPickSchema emits")

	assert.NotEqual(t,
		hashPrompt(extractionHashInput(json.RawMessage(pinningEnum), passage)),
		hashPrompt(extractionHashInput(json.RawMessage(decliningEnum), passage)),
		"same prompt text, changed schema: the hash must move")
}

// TestExtractionHashIsSentinelBuilt pins what production hashes. The stamp must
// name the prompt version and nothing about who was in the room: building the
// schemas from a real roster would give every teacher their own hash and make
// the stamp useless for correlating quality.
func TestExtractionHashIsSentinelBuilt(t *testing.T) {
	sentinel := extractionHashInput(classPickSchema(sentinelClasses), passageSchema(sentinelClasses[0]))
	assert.Equal(t, hashPrompt(sentinel), ExtractionPromptHash,
		"production hash must be the sentinel-built one")

	realRoster := extractionHashInput(classPickSchema(testClasses()), passageSchema(testClasses()[0]))
	assert.NotEqual(t, hashPrompt(realRoster), ExtractionPromptHash,
		"a real roster would otherwise move the hash, which is what the sentinel prevents")

	for _, c := range testClasses() {
		assert.NotContains(t, sentinel, c.Name, "a real class name reached the hash input")
		for _, s := range c.Students {
			assert.NotContains(t, sentinel, s.Name, "a real student name reached the hash input")
		}
	}
}
