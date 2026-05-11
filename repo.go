package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Repo struct {
	root string
}

func NewRepo(root string) (*Repo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo path is not a directory: %s", abs)
	}
	return &Repo{root: abs}, nil
}

func (r *Repo) Root() string { return r.root }

func (r *Repo) resolve(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", errors.New("path must be relative to repo root")
	}
	abs := filepath.Join(r.root, filepath.Clean(rel))
	rel2, err := filepath.Rel(r.root, abs)
	if err != nil || strings.HasPrefix(rel2, "..") {
		return "", fmt.Errorf("path escapes repo root: %s", rel)
	}
	return abs, nil
}

func (r *Repo) ReadFile(rel string) (string, error) {
	abs, err := r.resolve(rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Repo) ListFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(r.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || strings.HasPrefix(name, ".") && path != r.root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(r.root, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

func (r *Repo) Grep(pattern string, maxHits int) ([]string, error) {
	files, err := r.ListFiles()
	if err != nil {
		return nil, err
	}
	pattern = strings.ToLower(pattern)
	var hits []string
	for _, f := range files {
		content, err := r.ReadFile(f)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(content, "\n") {
			if strings.Contains(strings.ToLower(line), pattern) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", f, i+1, strings.TrimSpace(line)))
				if len(hits) >= maxHits {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}
