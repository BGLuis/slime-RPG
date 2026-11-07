package main

import (
	"io"
	"strings"
	"testing"
)

func TestConfirmReview(t *testing.T) {
	for in, want := range map[string]bool{"\n": true, "": true, "s\n": true, "n\n": false, "N\n": false, "não\n": false} {
		if got := confirmReview(strings.NewReader(in), io.Discard, "/x"); got != want {
			t.Errorf("entrada %q: got %v, want %v", in, got, want)
		}
	}
}
