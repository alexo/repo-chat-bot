package main

import (
	"strings"
	"testing"
)

func TestChunkForTelegram(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit int
		want  []string
	}{
		{
			name:  "short string fits in one chunk",
			input: "hello",
			limit: 100,
			want:  []string{"hello"},
		},
		{
			name:  "string exactly at limit",
			input: "12345",
			limit: 5,
			want:  []string{"12345"},
		},
		{
			name:  "splits on paragraph boundary",
			input: "para1 line\n\npara2 line\n\npara3 line",
			limit: 18,
			want:  []string{"para1 line\n\n", "para2 line\n\n", "para3 line"},
		},
		{
			name:  "splits on line boundary when no paragraph",
			input: "line1\nline2\nline3",
			limit: 8,
			want:  []string{"line1\n", "line2\n", "line3"},
		},
		{
			name:  "splits at exact limit when no break",
			input: "abcdefghij",
			limit: 4,
			want:  []string{"abcd", "efgh", "ij"},
		},
		{
			name:  "empty string returns single empty chunk",
			input: "",
			limit: 10,
			want:  []string{""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chunkForTelegram(tc.input, tc.limit)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d chunks, want %d:\n got:  %q\n want: %q", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("chunk %d: got %q, want %q", i, got[i], tc.want[i])
				}
				if len(got[i]) > tc.limit && len(tc.want) > 1 {
					t.Errorf("chunk %d exceeds limit %d: len=%d", i, tc.limit, len(got[i]))
				}
			}
			if joined := strings.Join(got, ""); joined != tc.input {
				t.Errorf("re-joined chunks %q != input %q", joined, tc.input)
			}
		})
	}
}

func TestLastIndexBefore(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		sep    string
		before int
		want   int
	}{
		{"finds last occurrence before bound", "ab\ncd\nef", "\n", 5, 2},
		{"returns -1 when separator absent", "abcdef", "\n", 10, -1},
		{"returns -1 when only occurrence is past bound", "abc\ndef", "\n", 3, -1},
		{"clamps when before exceeds length", "abc\n", "\n", 100, 3},
		{"multi-char separator picks last", "aa\n\nbb\n\ncc", "\n\n", 10, 6},
		{"separator at position 0", "\nabc", "\n", 5, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lastIndexBefore(tc.s, tc.sep, tc.before)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}
