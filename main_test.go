package main

import "testing"

func TestTextArg(t *testing.T) {
	tests := map[string]string{
		"make done":                "make done",
		"--position=top-left done": " --position=top-left done",
		"-x failed (exit 1)":       " -x failed (exit 1)",
		"":                         "",
	}
	for in, want := range tests {
		if got := textArg(in); got != want {
			t.Errorf("textArg(%q) = %q, want %q", in, got, want)
		}
	}
}
