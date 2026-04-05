package handler

import "testing"

func TestIsAllowedAudioType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"audio/mpeg", true},
		{"audio/wav", true},
		{"audio/mp4", true},
		{"audio/webm", true},
		{"video/webm", true},
		{"Audio/MPEG", true},
		{"video/mp4", false},
		{"application/pdf", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isAllowedAudioType(tt.ct); got != tt.want {
			t.Errorf("isAllowedAudioType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}
