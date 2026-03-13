package tts

import "context"

// Model abstracts a TTS model that converts text into audio data (e.g. PCM/WAV/MP3).
type Model interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// DummyModel is a reference implementation that returns an empty byte slice, useful as a placeholder or for tests.
type DummyModel struct{}

func (DummyModel) Synthesize(ctx context.Context, text string) ([]byte, error) {
	_ = ctx
	_ = text
	return []byte{}, nil
}

