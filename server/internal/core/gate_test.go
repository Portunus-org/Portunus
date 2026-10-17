package core

import (
	"regexp"
	"testing"
)

// A secret path is one word of the list, never one of the paths in use.
func TestNewGate(t *testing.T) {
	if len(gateWords) < 5000 {
		t.Fatalf("only %d words to choose from", len(gateWords))
	}
	shape := regexp.MustCompile(`^[a-z]{4,8}$`)
	for _, w := range gateWords {
		if !shape.MatchString(w) || gateReserved[w] {
			t.Fatalf("word %q must not be a secret path", w)
		}
	}
	seen := map[string]bool{}
	for range 50 {
		seen[newGate()] = true
	}
	if len(seen) < 40 {
		t.Fatalf("only %d different paths in 50", len(seen))
	}
}
