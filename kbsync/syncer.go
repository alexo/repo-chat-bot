package kbsync

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alexo/repo-chat-bot/kbsync/provider"
)

const (
	manifestFile      = ".kbsync.json"
	currentLinkName   = "current"
	snapshotPrefix    = "kb."
	defaultKeep       = 2
	defaultInterval   = time.Hour
)

type Options struct {
	BaseDir       string        // directory that holds kb.<ts>/ and the `current` symlink
	Provider      provider.Provider // backend
	Interval      time.Duration // poll cadence (used by Start)
	Delete        bool          // mirror mode: remove local files not in remote
	KeepSnapshots int           // how many old snapshots to retain (default 2)
	Logger        *log.Logger   // optional; defaults to log.Default()
}

type Syncer struct {
	opts Options
}

func NewSyncer(opts Options) *Syncer {
	if opts.KeepSnapshots <= 0 {
		opts.KeepSnapshots = defaultKeep
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	return &Syncer{opts: opts}
}

// CurrentPath returns the path that consumers should treat as the knowledge
// base root — the symlink, not the resolved snapshot. Resolving on each read
// means we pick up swaps without needing notification.
func (s *Syncer) CurrentPath() string {
	return filepath.Join(s.opts.BaseDir, currentLinkName)
}

// Start blocks until ctx is cancelled, running SyncNow on each tick. Errors
// are logged; they do not stop the loop.
func (s *Syncer) Start(ctx context.Context) {
	t := time.NewTicker(s.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.SyncNow(ctx); err != nil {
				s.opts.Logger.Printf("kbsync: %v", err)
			}
		}
	}
}

func (s *Syncer) SyncNow(ctx context.Context) error {
	if err := os.MkdirAll(s.opts.BaseDir, 0o755); err != nil {
		return fmt.Errorf("kbsync: mkdir base: %w", err)
	}

	remote, err := s.opts.Provider.List(ctx)
	if err != nil {
		return fmt.Errorf("kbsync: list: %w", err)
	}

	prev := s.resolveCurrent()
	manifest := s.loadCurrentManifest(prev)

	toFetch, toDelete := Diff(remote, manifest)
	if !s.opts.Delete {
		toDelete = nil
	}

	// Nothing to do: bucket already matches local. Skip the swap.
	if len(toFetch) == 0 && len(toDelete) == 0 {
		return nil
	}

	next := filepath.Join(s.opts.BaseDir, snapshotPrefix+time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.MkdirAll(next, 0o755); err != nil {
		return fmt.Errorf("kbsync: mkdir snapshot: %w", err)
	}
	cleanup := next // removed unless we commit

	defer func() {
		if cleanup != "" {
			_ = os.RemoveAll(cleanup)
		}
	}()

	// Carry forward files we already have locally (additive or mirrored).
	carry := map[string]bool{}
	for k := range manifest.Entries {
		carry[k] = true
	}
	for _, k := range toDelete {
		delete(carry, k)
	}
	for _, o := range toFetch {
		delete(carry, o.Key)
	}
	if prev != "" {
		for k := range carry {
			if err := linkInto(prev, next, k); err != nil {
				return fmt.Errorf("kbsync: carry %s: %w", k, err)
			}
		}
	}

	// Pull new/changed objects.
	for _, o := range toFetch {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.downloadInto(ctx, next, o.Key); err != nil {
			return fmt.Errorf("kbsync: download %s: %w", o.Key, err)
		}
	}

	// Write the manifest reflecting the new tree's contents.
	newEntries := map[string]string{}
	for _, o := range remote {
		newEntries[o.Key] = o.ETag
	}
	if !s.opts.Delete {
		for k, v := range manifest.Entries {
			if _, ok := newEntries[k]; !ok {
				newEntries[k] = v
			}
		}
	}
	if err := SaveManifest(filepath.Join(next, manifestFile), Manifest{Entries: newEntries}); err != nil {
		return fmt.Errorf("kbsync: save manifest: %w", err)
	}

	if err := s.swapSymlink(filepath.Base(next)); err != nil {
		return fmt.Errorf("kbsync: swap: %w", err)
	}
	cleanup = "" // commit: don't remove the new snapshot

	s.gcOldSnapshots(filepath.Base(next), prev)

	s.opts.Logger.Printf("kbsync: synced (fetched=%d deleted=%d total=%d) -> %s",
		len(toFetch), len(toDelete), len(newEntries), filepath.Base(next))
	return nil
}

func (s *Syncer) resolveCurrent() string {
	link := s.CurrentPath()
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.opts.BaseDir, target)
	}
	if _, err := os.Stat(target); err != nil {
		return ""
	}
	return target
}

func (s *Syncer) loadCurrentManifest(prev string) Manifest {
	if prev == "" {
		return Manifest{Entries: map[string]string{}}
	}
	m, err := LoadManifest(filepath.Join(prev, manifestFile))
	if err != nil {
		s.opts.Logger.Printf("kbsync: manifest load failed, treating as empty: %v", err)
		return Manifest{Entries: map[string]string{}}
	}
	return m
}

func (s *Syncer) downloadInto(ctx context.Context, snapshot, key string) error {
	dst := filepath.Join(snapshot, filepath.FromSlash(key))
	if rel, err := filepath.Rel(snapshot, dst); err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("key escapes snapshot: %s", key)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := tmpName
	defer func() {
		if cleanup != "" {
			_ = os.Remove(cleanup)
		}
	}()
	if err := s.opts.Provider.Download(ctx, key, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = ""
	return nil
}

// linkInto hard-links src/key into dst/key, falling back to copy on FS that
// disallow cross-device links (shouldn't happen since they share a parent).
func linkInto(src, dst, key string) error {
	sp := filepath.Join(src, filepath.FromSlash(key))
	dp := filepath.Join(dst, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dp), 0o755); err != nil {
		return err
	}
	if err := os.Link(sp, dp); err == nil {
		return nil
	}
	// Fallback: copy.
	in, err := os.Open(sp)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (s *Syncer) swapSymlink(targetName string) error {
	tmp := filepath.Join(s.opts.BaseDir, currentLinkName+".tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(targetName, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.opts.BaseDir, currentLinkName))
}

func (s *Syncer) gcOldSnapshots(currentName, prevPath string) {
	entries, err := os.ReadDir(s.opts.BaseDir)
	if err != nil {
		return
	}
	var snapshots []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), snapshotPrefix) {
			snapshots = append(snapshots, e.Name())
		}
	}
	sort.Strings(snapshots) // timestamp prefix sorts chronologically

	// Keep N newest. The current one always counts.
	keep := map[string]bool{currentName: true}
	if prevPath != "" {
		keep[filepath.Base(prevPath)] = true // give in-flight readers a chance to finish
	}
	for i := len(snapshots) - 1; i >= 0 && len(keep) < s.opts.KeepSnapshots; i-- {
		keep[snapshots[i]] = true
	}
	for _, name := range snapshots {
		if !keep[name] {
			_ = os.RemoveAll(filepath.Join(s.opts.BaseDir, name))
		}
	}
}
