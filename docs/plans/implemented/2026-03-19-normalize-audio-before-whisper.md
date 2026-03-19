# Fix audio filename extension for Whisper based on magic bytes

## Goal
Handle files where the extension doesn't match the actual codec (e.g., `.mp3` files that are really 3GPP/AAC containers from Android phones). Whisper uses the filename extension to determine format, so we need to fix it.

## Approach
Detect the real container format from magic bytes and override the filename extension before sending to Whisper. No transcoding needed — Whisper supports all these formats natively, it just needs the right extension.

## Magic bytes to detect

| Bytes | Format | Extension |
|-------|--------|-----------|
| `ID3` or `\xFF\xFB` / `\xFF\xF3` / `\xFF\xF2` | MP3 | `.mp3` |
| `ftyp` at offset 4 | MP4/M4A/3GP | `.m4a` |
| `RIFF....WAVE` | WAV | `.wav` |
| `OggS` | OGG | `.ogg` |
| `fLaC` | FLAC | `.flac` |
| `\x1A\x45\xDF\xA3` | WebM/Matroska | `.webm` |

## Proposed changes

### `backend/audio_format.go` (new file)
- `detectAudioExt(header []byte) string` — returns correct extension from magic bytes, or empty string if unknown.
- `fixAudioFilename(filename string, header []byte) string` — replaces extension if detection succeeds.

### `backend/audio_format_test.go` (new file)
- Table-driven tests with sample magic bytes for each format.

### `backend/deps.go`
- In `whisperTranscriber.Transcribe()`, read the first 12 bytes, detect format, fix filename, then pass a `io.MultiReader(header, remaining)` to Whisper so no bytes are lost.

## No deployment changes needed
Pure Go, no external dependencies.
