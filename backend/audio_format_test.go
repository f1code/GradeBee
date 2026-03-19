package handler

import (
	"bytes"
	"io"
	"testing"
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
			if got != tt.want {
				t.Errorf("detectAudioExt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFixAudioFilename(t *testing.T) {
	// 3GP file with .mp3 extension -> should become .m4a
	header3gp := []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', '3', 'g', 'p', '4'}
	got := fixAudioFilename("recording.mp3", header3gp)
	if got != "recording.m4a" {
		t.Errorf("fixAudioFilename() = %q, want %q", got, "recording.m4a")
	}

	// Actual MP3 stays .mp3
	headerMP3 := []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00")
	got = fixAudioFilename("song.mp3", headerMP3)
	if got != "song.mp3" {
		t.Errorf("fixAudioFilename() = %q, want %q", got, "song.mp3")
	}

	// Unknown format keeps original name
	got = fixAudioFilename("file.xyz", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	if got != "file.xyz" {
		t.Errorf("fixAudioFilename() = %q, want %q", got, "file.xyz")
	}
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
			if got := is3GPContainer(tt.header); got != tt.want {
				t.Errorf("is3GPContainer() = %v, want %v", got, tt.want)
			}
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
	if err != nil {
		t.Fatal(err)
	}

	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	// Check major brand is now "isom"
	if string(result[8:12]) != "isom" {
		t.Errorf("major brand = %q, want %q", string(result[8:12]), "isom")
	}

	// Check mdat box is preserved after ftyp (size field at 24, type at 28)
	if string(result[28:32]) != "mdat" {
		t.Errorf("mdat not found at expected offset, got %q", string(result[28:32]))
	}

	// Total length should be same as original
	if len(result) != len(full) {
		t.Errorf("result length = %d, want %d", len(result), len(full))
	}
}

func TestPeekReader(t *testing.T) {
	data := []byte("hello world, this is a test")
	header, remaining, err := peekReader(bytes.NewReader(data), 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(header) != 12 {
		t.Fatalf("header len = %d, want 12", len(header))
	}
	if string(header) != "hello world," {
		t.Errorf("header = %q, want %q", header, "hello world,")
	}
	// replayReader should reconstruct the full stream
	all, err := io.ReadAll(replayReader(header, remaining))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(all, data) {
		t.Errorf("replayed reader produced %q, want %q", all, data)
	}
}
