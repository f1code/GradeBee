// transcriber.go defines the Transcriber interface and its production
// implementation backed by the OpenAI Whisper API.
package handler

import (
	"context"
	"fmt"
	"io"
	"os"

	openai "github.com/sashabaranov/go-openai"
)

// Transcriber abstracts audio-to-text transcription for testability.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, audio io.Reader, prompt string) (string, error)
}

// whisperTranscriber uses the OpenAI Whisper API.
type whisperTranscriber struct {
	client *openai.Client
}

// newWhisperTranscriber creates a whisperTranscriber, reading the API key from
// the environment.
func newWhisperTranscriber() (Transcriber, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &whisperTranscriber{client: openai.NewClient(key)}, nil
}

func (w *whisperTranscriber) Transcribe(ctx context.Context, filename string, audio io.Reader, prompt string) (string, error) {
	// Peek at magic bytes to detect the real audio format and fix the
	// filename extension so Whisper parses the stream correctly.
	header, audio, err := peekReader(audio, 12)
	if err != nil {
		return "", fmt.Errorf("failed to read audio header: %w", err)
	}
	filename = fixAudioFilename(filename, header)

	// 3GPP containers are structurally identical to MP4 but Whisper rejects
	// them. Patch the ftyp major brand from "3gp*" to "isom".
	if is3GPContainer(header) {
		audio, err = patch3GPFtyp(header, audio)
		if err != nil {
			return "", fmt.Errorf("failed to patch 3GP container: %w", err)
		}
	} else {
		audio = replayReader(header, audio)
	}

	resp, err := w.client.CreateTranscription(ctx, openai.AudioRequest{
		Model:    openai.Whisper1,
		FilePath: filename,
		Reader:   audio,
		Prompt:   prompt,
	})
	if err != nil {
		return "", fmt.Errorf("whisper transcription failed: %w", err)
	}
	return resp.Text, nil
}
