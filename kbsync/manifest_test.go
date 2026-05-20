package kbsync

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alexo/repo-chat-bot/kbsync/provider"
)

func TestDiff(t *testing.T) {
	t.Run("first sync, empty manifest", func(t *testing.T) {
		remote := []provider.ObjectMeta{
			{Key: "a.md", ETag: "1"},
			{Key: "b.md", ETag: "2"},
		}
		fetch, del := Diff(remote, Manifest{})
		if len(fetch) != 2 || len(del) != 0 {
			t.Fatalf("got fetch=%d del=%d, want fetch=2 del=0", len(fetch), len(del))
		}
	})

	t.Run("unchanged etags skipped", func(t *testing.T) {
		remote := []provider.ObjectMeta{
			{Key: "a.md", ETag: "1"},
			{Key: "b.md", ETag: "2"},
		}
		m := Manifest{Entries: map[string]string{"a.md": "1", "b.md": "2"}}
		fetch, del := Diff(remote, m)
		if len(fetch) != 0 || len(del) != 0 {
			t.Fatalf("got fetch=%d del=%d, want both 0", len(fetch), len(del))
		}
	})

	t.Run("changed etag triggers refetch", func(t *testing.T) {
		remote := []provider.ObjectMeta{{Key: "a.md", ETag: "2"}}
		m := Manifest{Entries: map[string]string{"a.md": "1"}}
		fetch, del := Diff(remote, m)
		if len(fetch) != 1 || fetch[0].Key != "a.md" {
			t.Fatalf("expected single refetch of a.md, got %+v", fetch)
		}
		if len(del) != 0 {
			t.Fatalf("expected no deletions, got %v", del)
		}
	})

	t.Run("missing from remote queued for deletion", func(t *testing.T) {
		remote := []provider.ObjectMeta{{Key: "a.md", ETag: "1"}}
		m := Manifest{Entries: map[string]string{"a.md": "1", "old.md": "9"}}
		fetch, del := Diff(remote, m)
		if len(fetch) != 0 {
			t.Fatalf("expected no fetches, got %+v", fetch)
		}
		if !reflect.DeepEqual(del, []string{"old.md"}) {
			t.Fatalf("expected delete=[old.md], got %v", del)
		}
	})

	t.Run("mixed add/change/remove", func(t *testing.T) {
		remote := []provider.ObjectMeta{
			{Key: "keep.md", ETag: "1"},
			{Key: "changed.md", ETag: "new"},
			{Key: "added.md", ETag: "x"},
		}
		m := Manifest{Entries: map[string]string{
			"keep.md":    "1",
			"changed.md": "old",
			"removed.md": "z",
		}}
		fetch, del := Diff(remote, m)

		fetchKeys := map[string]bool{}
		for _, o := range fetch {
			fetchKeys[o.Key] = true
		}
		want := map[string]bool{"changed.md": true, "added.md": true}
		if !reflect.DeepEqual(fetchKeys, want) {
			t.Errorf("fetch: got %v, want %v", fetchKeys, want)
		}
		if !reflect.DeepEqual(del, []string{"removed.md"}) {
			t.Errorf("del: got %v, want [removed.md]", del)
		}
	})
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	original := Manifest{Entries: map[string]string{
		"a.md":     "etag-a",
		"sub/b.md": "etag-b",
	}}
	if err := SaveManifest(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(loaded, original) {
		t.Errorf("round trip mismatch: got %+v, want %+v", loaded, original)
	}
}

func TestLoadManifestMissingFileReturnsEmpty(t *testing.T) {
	m, err := LoadManifest(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(m.Entries) != 0 {
		t.Errorf("expected empty manifest, got %+v", m)
	}
}
