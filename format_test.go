package main

import (
	"strings"
	"testing"
)

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{3, "3"},
		{173, "173"},
		{999, "999"},
		{1000, "1.0k"},
		{36403, "36.4k"},
		{620070, "620.1k"},
		{3219810, "3.2m"},
		{2599740, "2.6m"},
	}
	for _, tc := range cases {
		if got := formatTokens(tc.n); got != tc.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestClip(t *testing.T) {
	if clip("hello", 60) != "hello" {
		t.Fatal("short string should stay")
	}
	got := clip(strings.Repeat("a", 80), 60)
	n := 0
	for range got {
		n++
	}
	if n != 60 {
		t.Fatalf("clip len = %d, want 60", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing ellipsis: %q", got)
	}
}
