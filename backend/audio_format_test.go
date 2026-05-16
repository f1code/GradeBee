package handler

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectAudioExt(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   string
	}{
		{"mp3 ID3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00"), ".mp3"},
		{"mp3 sync", []byte{0xFF, 0xFB, 0x90, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, ".mp3"},
		{"m4a ftyp", []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, ".m4a"},
		{"3gp ftyp", []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', '3', 'g', 'p', '4'}, ".m4a"},
		{"wav", []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'A', 'V', 'E'}, ".wav"},
		{"ogg", []byte{'O', 'g', 'g', 'S', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, ".ogg"},
		{"flac", []byte{'f', 'L', 'a', 'C', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, ".flac"},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, ".webm"},
		{"unknown", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, ""},
		{"too short", []byte{0x00, 0x00}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectAudioExt(tt.header)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFixAudioFilename(t *testing.T) {
	// 3GP file with .mp3 extension -> should become .m4a
	header3gp := []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', '3', 'g', 'p', '4'}
	assert.Equal(t, "recording.m4a", fixAudioFilename("recording.mp3", header3gp))

	// Actual MP3 stays .mp3
	headerMP3 := []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00")
	assert.Equal(t, "song.mp3", fixAudioFilename("song.mp3", headerMP3))

	// Unknown format keeps original name
	assert.Equal(t, "file.xyz", fixAudioFilename("file.xyz", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}))
}

func TestIs3GPContainer(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   bool
	}{
		{"3gp4", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', '3', 'g', 'p', '4'}, true},
		{"3gp5", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', '3', 'g', 'p', '5'}, true},
		{"3g2a", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', '3', 'g', '2', 'a'}, true},
		{"isom", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, false},
		{"M4A", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, false},
		{"not ftyp", []byte{0x00, 0x00, 0x00, 0x18, 'x', 'x', 'x', 'x', '3', 'g', 'p', '4'}, false},
		{"too short", []byte{0x00, 0x00, 0x00}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, is3GPContainer(tt.header))
		})
	}
}

func TestPatch3GPFtyp(t *testing.T) {
	// Simulate a 3GP ftyp box: size=24, "ftyp", "3gp4", minorVer=0, compat="isom","3gp4"
	// Then some mdat data after.
	ftyp := []byte{
		0x00, 0x00, 0x00, 0x18, // size = 24
		'f', 't', 'y', 'p',
		'3', 'g', 'p', '4', // major brand
		0x00, 0x00, 0x00, 0x00, // minor version
		'i', 's', 'o', 'm', // compat brand
		'3', 'g', 'p', '4', // compat brand
	}
	mdat := []byte{0x00, 0x00, 0x00, 0x01, 'm', 'd', 'a', 't'}
	full := make([]byte, 0, len(ftyp)+len(mdat))
	full = append(full, ftyp...)
	full = append(full, mdat...)

	// peekReader returns header (first 12 bytes) and remaining (from byte 12 onward)
	header := full[:12]
	rest := bytes.NewReader(full[12:])

	reader, err := patch3GPFtyp(header, rest)
	require.NoError(t, err)

	result, err := io.ReadAll(reader)
	require.NoError(t, err)

	// Check major brand is now "isom"
	assert.Equal(t, "isom", string(result[8:12]), "major brand should be isom")

	// Check mdat box is preserved after ftyp (size field at 24, type at 28)
	assert.Equal(t, "mdat", string(result[28:32]), "mdat not found at expected offset")

	// Total length should be same as original
	assert.Len(t, result, len(full))
}

func TestPeekReader(t *testing.T) {
	data := []byte("hello world, this is a test")
	header, remaining, err := peekReader(bytes.NewReader(data), 12)
	require.NoError(t, err)
	require.Len(t, header, 12)
	assert.Equal(t, "hello world,", string(header))

	// replayReader should reconstruct the full stream
	all, err := io.ReadAll(replayReader(header, remaining))
	require.NoError(t, err)
	assert.Equal(t, data, all)
}
