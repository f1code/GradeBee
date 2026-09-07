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

// TestClassGroupIDNeverReachesThePrompt pins that ClassGroup.ID — the row the
// done card's student picker needs (#134) — is invisible to the model. The
// prompt builders read Name only, so the text is byte-identical with and
// without it, and the hash is built from sentinelClasses, which carry none.
func TestClassGroupIDNeverReachesThePrompt(t *testing.T) {
	bare := testClasses()
	withIDs := testClasses()
	for i := range withIDs {
		withIDs[i].ID = int64(100 + i)
	}

	assert.Equal(t, BuildClassPickPrompt(bare), BuildClassPickPrompt(withIDs))
	assert.Equal(t, BuildPassagePrompt(bare[0]), BuildPassagePrompt(withIDs[0]))
	assert.Equal(t, string(classPickSchema(bare)), string(classPickSchema(withIDs)))
	assert.Equal(t, string(passageSchema(bare[0])), string(passageSchema(withIDs[0])))

	for _, c := range sentinelClasses {
		assert.Zero(t, c.ID, "the hash roster carries no row id")
	}
	assert.Equal(t,
		hashPrompt(extractionHashInput(classPickSchema(withIDs), passageSchema(withIDs[0]))),
		hashPrompt(extractionHashInput(classPickSchema(bare), passageSchema(bare[0]))),
		"an id on the roster must not move the hash")
}
