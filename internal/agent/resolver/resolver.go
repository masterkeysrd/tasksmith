package resolver

import (
	"context"
	"io"
	"path/filepath"

	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
	"github.com/masterkeysrd/warp"
)

// FileStorage defines the interface required by the resolver to save binary snapshots.
type FileStorage interface {
	Save(ctx context.Context, name string, r io.Reader) (string, error)
}

// Workspace defines the workspace interface required by the resolver to resolve
// agents and list resources for skill and context resolution.
type Workspace interface {
	ResolveAgent(ctx context.Context, ref string) (*warp.ResolvedAgent, error)
	Resources() []warp.Resource
	Providers() []*warp.ModelProvider
	CWD() string
	ResolveDefaults(ctx context.Context) (agentName, providerName, modelName string, err error)
	Contexts() []*warp.Context
	WorkspaceSpec() *warp.Workspace
	Project() *warp.Project
	Agents() []*warp.Agent
}

// Config holds the dependencies and configuration for the Resolver.
type Config struct {
	Lsp         *lsp.Manager
	Cwd         string
	FileTracker filetrack.FileTracker
	Storage     FileStorage
	Workspace   Workspace
}

// ResourceType defines the category of the resolved context reference.
type ResourceType string

const (
	TypeFile   ResourceType = "file"
	TypeSymbol ResourceType = "symbol"
	TypeSkill  ResourceType = "skill"
)

// ResolvedResource is the polymorphic interface implemented by all resolved workspace resources.
type ResolvedResource interface {
	Type() ResourceType
	Handle() string
	Path() string
}

// ResolvedFile holds the raw structured data of a resolved workspace file.
type ResolvedFile struct {
	FilePath    string
	Content     string
	StartLine   int
	EndLine     int
	TotalLines  int
	Truncated   bool
	MimeType    string
	IsBinary    bool
	IsDir       bool
	CachedPath  string
	Diagnostics []lsp.Diagnostic
}

func (f *ResolvedFile) Type() ResourceType { return TypeFile }
func (f *ResolvedFile) Handle() string     { return filepath.Base(f.FilePath) }
func (f *ResolvedFile) Path() string       { return f.FilePath }

// ResolvedSymbol holds the raw structured data of a resolved LSP symbol.
type ResolvedSymbol struct {
	Name                 string
	Kind                 string
	Signature            string
	TypeDefinedAt        string
	Container            string
	FilePath             string
	StartLine            int
	EndLine              int
	Snippet              string
	Diagnostics          []lsp.Diagnostic
	Docs                 string
	DocsTruncated        bool
	References           []string
	ReferencesTotal      int
	Implementations      []string
	ImplementationsTotal int
	FullReportPath       string
}

func (s *ResolvedSymbol) Type() ResourceType { return TypeSymbol }
func (s *ResolvedSymbol) Handle() string     { return s.Name }
func (s *ResolvedSymbol) Path() string       { return s.FilePath }

// ResolvedSkill holds the raw structured data of a resolved workspace agent skill.
type ResolvedSkill struct {
	Name         string
	SkillPath    string
	Instructions string
}

func (sk *ResolvedSkill) Type() ResourceType { return TypeSkill }
func (sk *ResolvedSkill) Handle() string     { return sk.Name }
func (sk *ResolvedSkill) Path() string       { return sk.SkillPath }

// Resolver orchestrates loading raw structured resource data from various workspace sources.
type Resolver struct {
	Lsp         *lsp.Manager
	Cwd         string
	FileTracker filetrack.FileTracker
	Storage     FileStorage
	Workspace   Workspace
}

// New creates a new Resolver instance with the provided configuration.
func New(cfg Config) *Resolver {
	return &Resolver{
		Lsp:         cfg.Lsp,
		Cwd:         cfg.Cwd,
		FileTracker: cfg.FileTracker,
		Storage:     cfg.Storage,
		Workspace:   cfg.Workspace,
	}
}

// EmbedThreshold is the maximum character count for symbols and skills to be
// embedded inline in the prompt. Files use the Truncated field instead.
const EmbedThreshold = 4000 // characters (~1000 tokens)

// ShouldEmbed determines whether the resource content is small enough to be
// fully embedded in the prompt message to save roundtrips, or if it should
// be referenced via metadata only to optimize token efficiency.
func (r *Resolver) ShouldEmbed(res ResolvedResource) bool {
	if res == nil {
		return false
	}

	switch val := res.(type) {
	case *ResolvedFile:
		// Do not embed binary files or directories
		if val.IsBinary || val.IsDir {
			return false
		}
		// Embed only when we have the complete file content. readTextFile sets
		// Truncated=true when the file exceeded MaxTotalChars, meaning we only
		// have a partial view — in that case, reference it so the agent knows
		// to call view_file for the rest.
		return len(val.Content) > 0 && !val.Truncated

	case *ResolvedSymbol:
		return len(val.Snippet) > 0

	case *ResolvedSkill:
		return len(val.Instructions) > 0
	}

	return false
}
