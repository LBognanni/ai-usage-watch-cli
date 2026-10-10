// Package cache stores only the latest normalized quota, never raw responses.
package cache

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"aiu/internal/quota"
)

func Path() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aiu", "snapshot.json"), nil
}

func Read(path string) (quota.Snapshot, error) {
	var s quota.Snapshot
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return s, err
	}
	if len(data) > 1024*1024 {
		return s, errors.New("cache too large")
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Version != 1 {
		return s, errors.New("unsupported cache version")
	}
	return s, nil
}

func Write(path string, s quota.Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
