package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRepo(t *testing.T) {
	tmp := t.TempDir()

	t.Run("valid directory", func(t *testing.T) {
		r, err := NewRepo(tmp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Root() == "" {
			t.Fatal("Root() returned empty string")
		}
		if !filepath.IsAbs(r.Root()) {
			t.Errorf("Root() %q is not absolute", r.Root())
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		_, err := NewRepo(filepath.Join(tmp, "does-not-exist"))
		if err == nil {
			t.Fatal("expected error for missing path")
		}
	})

	t.Run("path is a file not a directory", func(t *testing.T) {
		f := filepath.Join(tmp, "file.txt")
		if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := NewRepo(f)
		if err == nil {
			t.Fatal("expected error when path is a file")
		}
	})
}

func TestRepoResolve(t *testing.T) {
	tmp := t.TempDir()
	r, err := NewRepo(tmp)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"normal relative path", "foo/bar.txt", false},
		{"single file at root", "README.md", false},
		{"internal traversal that stays inside", "foo/../bar.txt", false},
		{"current dir", ".", false},
		{"empty string resolves to root", "", false},
		{"absolute path rejected", "/etc/passwd", true},
		{"parent directory rejected", "..", true},
		{"escaping traversal rejected", "../etc/passwd", true},
		{"deeper escaping traversal rejected", "foo/../../etc/passwd", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.resolve(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("got path %q and nil error, want error", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !strings.HasPrefix(got, r.Root()) {
				t.Errorf("resolved path %q is not under root %q", got, r.Root())
			}
		})
	}
}
