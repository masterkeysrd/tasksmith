package fsutil

import (
	"fmt"
	"os"
)

// EnsureDir ensures that the specified directory exists, creating it if necessary.
func EnsureDir(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to check directory %s: %w", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path %s exists but is not a directory", path)
	}

	return nil
}
