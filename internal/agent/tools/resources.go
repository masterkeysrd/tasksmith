package tools

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/masterkeysrd/warp"
)

// Resources returns all builtin tool resources.
func Resources() ([]*warp.Tool, error) {
	var tools []*warp.Tool
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read tool %s: %w", path, err)
		}
		result, err := warp.Parse(path, string(data))
		if err != nil {
			return fmt.Errorf("parse tool %s: %w", path, err)
		}
		if t, ok := result.Resource.(*warp.Tool); ok {
			tools = append(tools, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tools, nil
}
