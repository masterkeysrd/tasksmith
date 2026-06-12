package workspace

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/masterkeysrd/warp"
)

//go:embed all:builtin/*.md
var builtinFS embed.FS

// systemProvider implements warp.ResourceProvider by loading embedded resources
// and returning them as a *warp.ResourceSet.
type systemProvider struct{}

// LoadResources loads all embedded system resources and dynamic tools.
func (p *systemProvider) LoadResources() (*warp.ResourceSet, error) {
	resources := &warp.ResourceSet{
		Agents:         make(map[string]*warp.Agent),
		Skills:         make(map[string]*warp.Skill),
		Commands:       make(map[string]*warp.Command),
		ModelProviders: make(map[string]*warp.ModelProvider),
		Tools:          make(map[string]*warp.Tool),
		MCPs:           make(map[string]*warp.MCP),
		Toolkits:       make(map[string]*warp.Toolkit),
	}

	err := fs.WalkDir(builtinFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".agent.md") && !strings.HasSuffix(path, ".provider.yaml") {
			return nil
		}
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		result, err := warp.Parse(path, string(data))
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		switch r := result.Resource.(type) {
		case *warp.Agent:
			resources.Agents[r.GetName()] = r
		case *warp.ModelProvider:
			resources.ModelProviders[r.GetName()] = r
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return resources, nil
}
