package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

type builtinFile struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Mode uint32 `json:"mode"`
}

type builtinManifest struct {
	Version  int                    `json:"version"`
	TreeHash string                 `json:"treeHash"`
	Files    map[string]builtinFile `json:"files"`
}

var builtinOnce sync.Once
var builtinSyncMu sync.Mutex
var embeddedManifest builtinManifest
var embeddedManifestErr error

func hashBuiltin(data []byte) string {
	sum := sha256.Sum256(data)
	return "h1:" + hex.EncodeToString(sum[:])
}

func makeBuiltinManifest(source fs.FS) (builtinManifest, error) {
	manifest := builtinManifest{Version: 1, Files: make(map[string]builtinFile)}
	var paths []string
	err := fs.WalkDir(source, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		mode := uint32(0644)
		for part := range strings.SplitSeq(path.Dir(name), "/") {
			if part == "scripts" || part == "bin" {
				mode = 0755
				break
			}
		}
		manifest.Files[name] = builtinFile{Hash: hashBuiltin(data), Size: int64(len(data)), Mode: mode}
		paths = append(paths, name)
		return nil
	})
	if err != nil {
		return manifest, err
	}
	slices.Sort(paths)
	root := sha256.New()
	for _, name := range paths {
		entry := manifest.Files[name]
		fmt.Fprintf(root, "%s\x00%04o\x00%d\x00%s\n", name, entry.Mode, entry.Size, entry.Hash)
	}
	manifest.TreeHash = "h1:" + hex.EncodeToString(root.Sum(nil))
	return manifest, nil
}

func EnsureBuiltinDir() (string, error) {
	builtinOnce.Do(func() {
		source, err := fs.Sub(builtinFS, "builtin")
		if err != nil {
			embeddedManifestErr = err
			return
		}
		embeddedManifest, embeddedManifestErr = makeBuiltinManifest(source)
	})
	if embeddedManifestErr != nil {
		return "", embeddedManifestErr
	}
	dir, err := xdg.BuiltinDir()
	if err != nil {
		return "", err
	}
	source, err := fs.Sub(builtinFS, "builtin")
	if err != nil {
		return "", err
	}
	builtinSyncMu.Lock()
	defer builtinSyncMu.Unlock()
	if err := syncBuiltinDir(dir, source, embeddedManifest); err != nil {
		return "", err
	}
	return dir, nil
}

func syncBuiltinDir(dir string, source fs.FS, manifest builtinManifest) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	var previous builtinManifest
	if data, err := root.ReadFile(".manifest.json"); err == nil {
		_ = json.Unmarshal(data, &previous)
	}
	changed := previous.Version != manifest.Version || previous.TreeHash != manifest.TreeHash
	for name, entry := range manifest.Files {
		info, statErr := root.Lstat(name)
		valid := statErr == nil && info.Mode().IsRegular() && info.Size() == entry.Size && uint32(info.Mode().Perm()) == entry.Mode
		if valid {
			data, readErr := root.ReadFile(name)
			valid = readErr == nil && hashBuiltin(data) == entry.Hash
		}
		if valid {
			continue
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.FromSlash(path.Dir(name)), 0755); err != nil {
			return err
		}
		if err := writeBuiltinAtomic(root, filepath.FromSlash(name), data, os.FileMode(entry.Mode)); err != nil {
			return err
		}
		changed = true
	}
	for name := range previous.Files {
		if _, ok := manifest.Files[name]; ok {
			continue
		}
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") || name == ".manifest.json" {
			continue
		}
		if err := root.Remove(filepath.FromSlash(name)); err != nil && !os.IsNotExist(err) {
			return err
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if err := root.Remove(filepath.FromSlash(parent)); err != nil {
				break
			}
		}
		changed = true
	}
	if !changed {
		return nil
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeBuiltinAtomic(root, ".manifest.json", append(data, '\n'), 0644)
}

func writeBuiltinAtomic(root *os.Root, name string, data []byte, mode os.FileMode) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(name), ".builtin-"+hex.EncodeToString(nonce[:]))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(mode)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return root.Rename(tmp, name)
}
