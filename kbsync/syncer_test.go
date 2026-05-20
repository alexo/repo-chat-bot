package kbsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alexo/repo-chat-bot/kbsync/provider"
)

// fakeProvider lets tests script bucket contents per sync.
type fakeProvider struct {
	objects  map[string]string // key -> body
	etags    map[string]string // key -> etag (defaults to body if unset)
	listErr  error
	dlErrFor map[string]error
}

func (p *fakeProvider) List(ctx context.Context) ([]provider.ObjectMeta, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	out := make([]provider.ObjectMeta, 0, len(p.objects))
	for k, body := range p.objects {
		etag := p.etags[k]
		if etag == "" {
			etag = body
		}
		out = append(out, provider.ObjectMeta{Key: k, ETag: etag, Size: int64(len(body))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (p *fakeProvider) Download(ctx context.Context, key string, w io.Writer) error {
	if err := p.dlErrFor[key]; err != nil {
		return err
	}
	body, ok := p.objects[key]
	if !ok {
		return fmt.Errorf("not found: %s", key)
	}
	_, err := io.WriteString(w, body)
	return err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func currentLinkTarget(t *testing.T, base string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(base, "current"))
	if err != nil {
		t.Fatalf("readlink current: %v", err)
	}
	return target
}

func TestSyncer_FirstSyncCreatesCurrent(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha", "sub/b.md": "beta"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := readFile(t, filepath.Join(base, "current", "a.md")); got != "alpha" {
		t.Errorf("a.md: %q want alpha", got)
	}
	if got := readFile(t, filepath.Join(base, "current", "sub", "b.md")); got != "beta" {
		t.Errorf("sub/b.md: %q want beta", got)
	}
}

func TestSyncer_NoOpWhenNothingChanged(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	first := currentLinkTarget(t, base)

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if currentLinkTarget(t, base) != first {
		t.Errorf("symlink swapped despite no changes: was %s, now %s", first, currentLinkTarget(t, base))
	}
}

func TestSyncer_DetectsChangedAndAddedFiles(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha", "b.md": "beta"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	p.objects["a.md"] = "alpha2"
	p.objects["c.md"] = "gamma"

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if got := readFile(t, filepath.Join(base, "current", "a.md")); got != "alpha2" {
		t.Errorf("a.md: %q want alpha2", got)
	}
	if got := readFile(t, filepath.Join(base, "current", "b.md")); got != "beta" {
		t.Errorf("b.md: %q want beta", got)
	}
	if got := readFile(t, filepath.Join(base, "current", "c.md")); got != "gamma" {
		t.Errorf("c.md: %q want gamma", got)
	}
}

func TestSyncer_MirrorModeDeletesRemovedFiles(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha", "b.md": "beta"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	delete(p.objects, "b.md")

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "current", "b.md")); !os.IsNotExist(err) {
		t.Errorf("b.md should be deleted in mirror mode, stat err=%v", err)
	}
}

func TestSyncer_AdditiveModeKeepsRemovedFiles(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha", "b.md": "beta"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: false})

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	delete(p.objects, "b.md")

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if got := readFile(t, filepath.Join(base, "current", "b.md")); got != "beta" {
		t.Errorf("b.md should survive additive sync, got %q", got)
	}
}

func TestSyncer_DownloadErrorLeavesCurrentIntact(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})

	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	prev := currentLinkTarget(t, base)

	p.objects["a.md"] = "alpha2"
	p.dlErrFor = map[string]error{"a.md": errors.New("boom")}

	if err := s.SyncNow(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	if got := readFile(t, filepath.Join(base, "current", "a.md")); got != "alpha" {
		t.Errorf("a.md should still be old content, got %q", got)
	}
	if currentLinkTarget(t, base) != prev {
		t.Errorf("symlink should not have moved on failed sync")
	}
}

func TestSyncer_GarbageCollectsOldSnapshots(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "v1"}}
	s := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true, KeepSnapshots: 2})

	for i := 0; i < 5; i++ {
		p.objects["a.md"] = fmt.Sprintf("v%d", i)
		if err := s.SyncNow(context.Background()); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var snapshots int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "kb.") {
			snapshots++
		}
	}
	if snapshots > 2 {
		t.Errorf("expected <=2 snapshots retained, got %d", snapshots)
	}
}

func TestSyncer_ManifestPersistedAcrossRestart(t *testing.T) {
	base := t.TempDir()
	p := &fakeProvider{objects: map[string]string{"a.md": "alpha"}}
	s1 := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})
	if err := s1.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	firstSnapshot := currentLinkTarget(t, base)

	// Fresh syncer, same base dir, same remote — should be a no-op.
	s2 := NewSyncer(Options{BaseDir: base, Provider: p, Delete: true})
	if err := s2.SyncNow(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if currentLinkTarget(t, base) != firstSnapshot {
		t.Errorf("manifest not respected across restart: snapshot changed")
	}
}
