package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/hash"
)

// Store manages immutable, content-addressed media blobs under <session-dir>/media/<sha256>.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir (typically <session-dir>/media).
func NewStore(dir string) (*Store, error) {
	clean := filepath.Clean(strings.TrimSpace(dir))
	if clean == "" || clean == "." {
		return nil, errors.New("media: store directory is required")
	}
	return &Store{dir: clean}, nil
}

// Dir returns the backing directory path on disk.
func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) pathFor(sha string) (string, error) {
	if !hash.IsValidSHA256(sha) {
		return "", fmt.Errorf("media: invalid sha256 %q", sha)
	}
	return filepath.Join(s.dir, sha), nil
}

// Put writes data into <dir>/<sha256> atomically if not already present,
// returning the lowercase hex SHA-256 digest and byte size.
func (s *Store) Put(ctx context.Context, data []byte) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	hexSum := hash.SHA256Hex(data)
	dst, err := s.pathFor(hexSum)
	if err != nil {
		return "", 0, err
	}
	if info, err := os.Stat(dst); err == nil && info.Mode().IsRegular() && info.Size() == int64(len(data)) {
		return hexSum, info.Size(), nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("media: create store dir: %w", err)
	}
	if err := fsutils.WriteFileAtomic(dst, data, 0o644); err != nil {
		// Another concurrent writer may have won the race for the same SHA-256.
		if info, serr := os.Stat(dst); serr == nil && info.Size() == int64(len(data)) {
			return hexSum, info.Size(), nil
		}
		return "", 0, fmt.Errorf("media: write blob: %w", err)
	}
	return hexSum, int64(len(data)), nil
}

// Read retrieves the bytes stored for sha256Hex.
func (s *Store) Read(sha256Hex string) ([]byte, error) {
	p, err := s.pathFor(sha256Hex)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("media: read blob %s: %w", sha256Hex, err)
	}
	return data, nil
}

// Exists reports whether sha256Hex is present in the store.
func (s *Store) Exists(sha256Hex string) bool {
	p, err := s.pathFor(sha256Hex)
	if err != nil {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// LinkAllFrom hardlinks all content-addressed media blobs from srcDir into s.dir
// in O(1), falling back to copying if the source and destination reside on
// different filesystems. Missing srcDir is a no-op.
func (s *Store) LinkAllFrom(srcDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("media: read source media dir: %w", err)
	}
	var createdDir bool
	for _, entry := range entries {
		name := entry.Name()
		if !hash.IsValidSHA256(name) || entry.IsDir() {
			continue
		}
		if !createdDir {
			if err := os.MkdirAll(s.dir, 0o755); err != nil {
				return fmt.Errorf("media: create fork media dir: %w", err)
			}
			createdDir = true
		}
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(s.dir, name)
		if _, err := os.Stat(dstPath); err == nil {
			continue
		}
		if err := fsutils.LinkOrCopy(srcPath, dstPath, 0o644); err != nil {
			return fmt.Errorf("media: link blob %s: %w", name, err)
		}
	}
	return nil
}
