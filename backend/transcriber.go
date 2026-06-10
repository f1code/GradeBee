// transcriber.go defines the Transcriber interface and its production
// implementation backed by an LLMProvider. Audio-format normalisation
// (3GP patching, extension fixing) lives here so both Whisper and Voxtral
// benefit without either provider needing to know about audio quirks.
package handler

import (
	"context"
	"fmt"
	"io"
)

// Transcriber abstracts audio-to-text transcription for testability.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, audio io.Reader, contextBias []string) (string, error)
}

// providerTranscriber delegates to an LLMProvider after normalising the audio
// format (3GP patching + extension fixing).
type providerTranscriber struct {
	provider LLMProvider
}

func newProviderTranscriber(provider LLMProvider) Transcriber {
	return &providerTranscriber{provider: provider}
}

func (t *providerTranscriber) Transcribe(ctx context.Context, filename string, audio io.Reader, contextBias []string) (string, error) {
	// Peek at magic bytes to detect the real audio format and fix the
	// filename extension so the transcription API parses the stream correctly.
	header, audio, err := peekReader(audio, 12)
	if err != nil {
		return "", fmt.Errorf("failed to read audio header: %w", err)
	}
	filename = fixAudioFilename(filename, header)

	// 3GPP containers are structurally identical to MP4 but some APIs reject
	// them. Patch the ftyp major brand from "3gp*" to "isom".
	if is3GPContainer(header) {
		audio, err = patch3GPFtyp(header, audio)
		if err != nil {
			return "", fmt.Errorf("failed to patch 3GP container: %w", err)
		}
	} else {
		audio = replayReader(header, audio)
	}

	resp, err := t.provider.Transcribe(ctx, TranscribeRequest{
		Filename:    filename,
		Audio:       audio,
		ContextBias: contextBias,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}
