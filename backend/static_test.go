package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCacheControlFor_ManifestJSON(t *testing.T) {
	assert.Equal(t, "no-cache", cacheControlFor("/manifest.json"))
}

func TestCacheControlFor_HashedAssets(t *testing.T) {
	assert.Equal(t, "public, max-age=31536000, immutable", cacheControlFor("/assets/index-abc.js"))
}

func TestCacheControlFor_OtherFiles(t *testing.T) {
	assert.Empty(t, cacheControlFor("/favicon.svg"))
	assert.Empty(t, cacheControlFor("/apple-touch-icon.png"))
}
