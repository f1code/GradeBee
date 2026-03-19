package handler

import (
	"bytes"
	"encoding/binary"
	"io"
	"path/filepath"
	"strings"
)

// detectAudioExt returns the correct file extension based on magic bytes.
// Returns empty string if format is unrecognized.
func detectAudioExt(header []byte) string {
	if len(header) < 4 {
		return ""
	}

	// MP3: ID3 tag or MPEG sync word
	if header[0] == 'I' && header[1] == 'D' && header[2] == '3' {
		return ".mp3"
	}
	if header[0] == 0xFF && (header[1]&0xE0) == 0xE0 {
		return ".mp3"
	}

	// MP4/M4A/3GP: "ftyp" at offset 4
	if len(header) >= 8 && string(header[4:8]) == "ftyp" {
		return ".m4a"
	}

	// WAV: RIFF....WAVE
	if len(header) >= 12 && string(header[0:4]) == "RIFF" && string(header[8:12]) == "WAVE" {
		return ".wav"
	}

	// OGG
	if string(header[0:4]) == "OggS" {
		return ".ogg"
	}

	// FLAC
	if string(header[0:4]) == "fLaC" {
		return ".flac"
	}

	// WebM/Matroska
	if header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3 {
		return ".webm"
	}

	return ""
}

// is3GPContainer returns true if the ftyp major brand is a 3GPP variant
// that Whisper doesn't accept.
func is3GPContainer(header []byte) bool {
	if len(header) < 12 || string(header[4:8]) != "ftyp" {
		return false
	}
	brand := string(header[8:12])
	return strings.HasPrefix(brand, "3gp") || strings.HasPrefix(brand, "3g2")
}

// patch3GPFtyp reads the ftyp box from a 3GPP stream and rewrites the major
// brand to "isom" so Whisper accepts it as MP4/M4A. Returns the patched header
// bytes and a reader for the remainder of the stream.
func patch3GPFtyp(header []byte, rest io.Reader) (io.Reader, error) {
	// ftyp box: [4-byte size][4-byte "ftyp"][4-byte major brand][4-byte minor version][compatible brands...]
	// We already have the first 12 bytes in header. Read the rest of the ftyp box.
	boxSize := int(binary.BigEndian.Uint32(header[0:4]))
	if boxSize < 12 || boxSize > 1024 {
		// Unexpected box size, return unmodified.
		return io.MultiReader(bytes.NewReader(header), rest), nil
	}

	remaining := boxSize - len(header)
	if remaining > 0 {
		extra := make([]byte, remaining)
		if _, err := io.ReadFull(rest, extra); err != nil {
			return nil, err
		}
		header = append(header, extra...)
	}

	// Patch major brand to "isom"
	copy(header[8:12], "isom")

	return io.MultiReader(bytes.NewReader(header), rest), nil
}

// fixAudioFilename replaces the file extension if magic bytes indicate a
// different format than the extension suggests.
func fixAudioFilename(filename string, header []byte) string {
	detected := detectAudioExt(header)
	if detected == "" {
		return filename
	}
	currentExt := strings.ToLower(filepath.Ext(filename))
	if currentExt == detected {
		return filename
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename)) + detected
}

// peekReader reads the first n bytes from r and returns them along with the
// original reader (which has been advanced past those bytes).
func peekReader(r io.Reader, n int) (header []byte, remaining io.Reader, err error) {
	buf := make([]byte, n)
	nRead, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, nil, err
	}
	return buf[:nRead], r, nil
}

// replayReader creates a reader that replays header bytes followed by the
// remaining stream.
func replayReader(header []byte, remaining io.Reader) io.Reader {
	return io.MultiReader(bytes.NewReader(header), remaining)
}
