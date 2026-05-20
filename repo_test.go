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

func TestRepoListFilesThroughSymlink(t *testing.T) {
	// kbsync points REPO_PATH at a symlink; ListFiles must descend through it.
	base := t.TempDir()
	target := filepath.Join(base, "kb.001")
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "sub", "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	link := filepath.Join(base, "current")
	if err := os.Symlink("kb.001", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	r, err := NewRepo(link)
	if err != nil {
		t.Fatalf("NewRepo via symlink: %v", err)
	}
	files, err := r.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[filepath.ToSlash(f)] = true
	}
	if !got["a.md"] || !got["sub/b.md"] {
		t.Errorf("ListFiles missed files via symlink, got %v", files)
	}
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
