package kbsync

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"

	"github.com/alexo/repo-chat-bot/kbsync/provider"
)

type Manifest struct {
	Entries map[string]string `json:"entries"`
}

func LoadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{Entries: map[string]string{}}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Entries == nil {
		m.Entries = map[string]string{}
	}
	return m, nil
}

func SaveManifest(path string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Diff returns the objects that must be downloaded and the keys that must be
// removed locally to bring the local mirror in sync with remote. mirror=false
// callers can simply ignore the second return.
func Diff(remote []provider.ObjectMeta, local Manifest) (toFetch []provider.ObjectMeta, toDelete []string) {
	seen := make(map[string]bool, len(remote))
	for _, o := range remote {
		seen[o.Key] = true
		if local.Entries[o.Key] != o.ETag {
			toFetch = append(toFetch, o)
		}
	}
	for k := range local.Entries {
		if !seen[k] {
			toDelete = append(toDelete, k)
		}
	}
	sort.Strings(toDelete)
	return toFetch, toDelete
}
