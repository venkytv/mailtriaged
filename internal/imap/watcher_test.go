package imap

import (
	"testing"
)

func TestBackoffDelay(t *testing.T) {
	backoff := []int{5, 15, 60, 300}

	tests := []struct {
		attempt int
		want    int
	}{
		{0, 5},
		{1, 15},
		{2, 60},
		{3, 300},
		{4, 300},  // clamped to last
		{10, 300}, // clamped to last
	}

	for _, tt := range tests {
		got := backoffDelay(backoff, tt.attempt)
		if got != tt.want {
			t.Errorf("backoffDelay(attempt=%d) = %d, want %d", tt.attempt, got, tt.want)
		}
	}
}
