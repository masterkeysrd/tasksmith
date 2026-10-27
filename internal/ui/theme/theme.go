package theme

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// Default is the name of the default theme.
const Default = "dark"

// Client represents the type of client consuming the theme.
type Client string

const (
	ClientCLI     Client = "cli"
	ClientTUI     Client = "tui"
	ClientDesktop Client = "desktop"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// Theme represents the JSON structure of a theme.
type Theme struct {
	Name        string                    `json:"name"`
	Extends     string                    `json:"extends,omitempty"`
	Type        string                    `json:"type"` // "dark" or "light"
	Transparent *bool                     `json:"transparent,omitempty"`
	Palette     map[string]string         `json:"palette,omitempty"`
	Colors      ThemeColors               `json:"colors"`
	Clients     map[Client]ClientOverride `json:"clients,omitempty"`
}

// ClientOverride represents client-specific overrides for a theme.
type ClientOverride struct {
	Transparent *bool             `json:"transparent,omitempty"`
	Palette     map[string]string `json:"palette,omitempty"`
	Colors      ThemeColors       `json:"colors"`
}

// ThemeColors represents all 11 token groups in theme configuration.
type ThemeColors struct {
	Text       ThemeTextColor       `json:"text"`
	Surface    ThemeSurfaceColor    `json:"surface"`
	Tint       ThemeTintColor       `json:"tint"`
	Border     ThemeBorderColor     `json:"border"`
	Icon       ThemeIconColor       `json:"icon"`
	Agent      ThemeAgentColor      `json:"agent"`
	Mode       ThemeModeColor       `json:"mode"`
	Diff       ThemeDiffColor       `json:"diff"`
	Transcript ThemeTranscriptColor `json:"transcript"`
	Meter      ThemeMeterColor      `json:"meter"`
	Syntax     ThemeSyntaxColor     `json:"syntax"`
}

type ThemeTextColor struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Muted     string `json:"muted"`
	Disabled  string `json:"disabled"`
	Inverse   string `json:"inverse"`
	Accent    string `json:"accent"`
	Info      string `json:"info"`
	Success   string `json:"success"`
	Warning   string `json:"warning"`
	Error     string `json:"error"`
}

type ThemeSurfaceColor struct {
	Canvas   string `json:"canvas"`
	Panel    string `json:"panel"`
	Elevated string `json:"elevated"`
	Code     string `json:"code"`
	Hover    string `json:"hover"`
	Active   string `json:"active"`
	Selected string `json:"selected"`
	Disabled string `json:"disabled"`
}

type ThemeTintColor struct {
	Accent  string `json:"accent"`
	Info    string `json:"info"`
	Success string `json:"success"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
}

type ThemeBorderColor struct {
	Default string `json:"default"`
	Subtle  string `json:"subtle"`
	Focus   string `json:"focus"`
	Accent  string `json:"accent"`
	Info    string `json:"info"`
	Success string `json:"success"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
}

type ThemeIconColor struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Muted     string `json:"muted"`
	Accent    string `json:"accent"`
	Info      string `json:"info"`
	Success   string `json:"success"`
	Warning   string `json:"warning"`
	Error     string `json:"error"`
}

type ThemeAgentColor struct {
	Brand      string              `json:"brand"`
	Thinking   string              `json:"thinking"`
	Generating string              `json:"generating"`
	Executing  string              `json:"executing"`
	Permission string              `json:"permission"`
	Subagents  ThemeSubagentColors `json:"subagents"`
}

type ThemeSubagentColors struct {
	Red    string `json:"red"`
	Blue   string `json:"blue"`
	Green  string `json:"green"`
	Yellow string `json:"yellow"`
	Purple string `json:"purple"`
	Orange string `json:"orange"`
	Pink   string `json:"pink"`
	Cyan   string `json:"cyan"`
}

type ThemeModeColor struct {
	Default     string `json:"default"`
	Plan        string `json:"plan"`
	AcceptEdits string `json:"accept_edits"`
	Auto        string `json:"auto"`
	Ask         string `json:"ask"`
	Bash        string `json:"bash"`
}

type ThemeDiffColor struct {
	AddedBg       string `json:"added_bg"`
	AddedText     string `json:"added_text"`
	AddedWord     string `json:"added_word"`
	AddedDimmed   string `json:"added_dimmed"`
	RemovedBg     string `json:"removed_bg"`
	RemovedText   string `json:"removed_text"`
	RemovedWord   string `json:"removed_word"`
	RemovedDimmed string `json:"removed_dimmed"`
	HunkBg        string `json:"hunk_bg"`
	HunkText      string `json:"hunk_text"`
}

type ThemeTranscriptColor struct {
	UserBg      string `json:"user_bg"`
	UserHoverBg string `json:"user_hover_bg"`
	BashBg      string `json:"bash_bg"`
	SystemText  string `json:"system_text"`
	QueuedText  string `json:"queued_text"`
}

type ThemeMeterColor struct {
	Fill   string `json:"fill"`
	Warn   string `json:"warn"`
	Danger string `json:"danger"`
	Empty  string `json:"empty"`
}

type ThemeSyntaxColor struct {
	Keyword     string `json:"keyword"`
	Function    string `json:"function"`
	String      string `json:"string"`
	Number      string `json:"number"`
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	Operator    string `json:"operator"`
	Variable    string `json:"variable"`
	Punctuation string `json:"punctuation"`
}

// Scheme represents the resolved theme values.
type Scheme struct {
	Name        string
	Type        string
	Transparent bool
	Palette     map[string]color.Color
	Color       Color
}

// Color represents the resolved theme colors.
type Color struct {
	Text       TextColor
	Surface    SurfaceColor
	Tint       TintColor
	Border     BorderColor
	Icon       IconColor
	Agent      AgentColor
	Mode       ModeColor
	Diff       DiffColor
	Transcript TranscriptColor
	Meter      MeterColor
	Syntax     SyntaxColor
}

type TextColor struct {
	Primary   color.Color
	Secondary color.Color
	Muted     color.Color
	Disabled  color.Color
	Inverse   color.Color
	Accent    color.Color
	Info      color.Color
	Success   color.Color
	Warning   color.Color
	Error     color.Color
}

type SurfaceColor struct {
	Canvas   color.Color
	Panel    color.Color
	Elevated color.Color
	Code     color.Color
	Hover    color.Color
	Active   color.Color
	Selected color.Color
	Disabled color.Color
}

type TintColor struct {
	Accent  color.Color
	Info    color.Color
	Success color.Color
	Warning color.Color
	Error   color.Color
}

type BorderColor struct {
	Default color.Color
	Subtle  color.Color
	Focus   color.Color
	Accent  color.Color
	Info    color.Color
	Success color.Color
	Warning color.Color
	Error   color.Color
}

type IconColor struct {
	Primary   color.Color
	Secondary color.Color
	Muted     color.Color
	Accent    color.Color
	Info      color.Color
	Success   color.Color
	Warning   color.Color
	Error     color.Color
}

type AgentColor struct {
	Brand      color.Color
	Thinking   color.Color
	Generating color.Color
	Executing  color.Color
	Permission color.Color
	Subagents  SubagentColors
}

type SubagentColors struct {
	Red    color.Color
	Blue   color.Color
	Green  color.Color
	Yellow color.Color
	Purple color.Color
	Orange color.Color
	Pink   color.Color
	Cyan   color.Color
}

type ModeColor struct {
	Default     color.Color
	Plan        color.Color
	AcceptEdits color.Color
	Auto        color.Color
	Ask         color.Color
	Bash        color.Color
}

type DiffColor struct {
	AddedBg       color.Color
	AddedText     color.Color
	AddedWord     color.Color
	AddedDimmed   color.Color
	RemovedBg     color.Color
	RemovedText   color.Color
	RemovedWord   color.Color
	RemovedDimmed color.Color
	HunkBg        color.Color
	HunkText      color.Color
}

type TranscriptColor struct {
	UserBg      color.Color
	UserHoverBg color.Color
	BashBg      color.Color
	SystemText  color.Color
	QueuedText  color.Color
}

type MeterColor struct {
	Fill   color.Color
	Warn   color.Color
	Danger color.Color
	Empty  color.Color
}

type SyntaxColor struct {
	Keyword     color.Color
	Function    color.Color
	String      color.Color
	Number      color.Color
	Type        color.Color
	Comment     color.Color
	Operator    color.Color
	Variable    color.Color
	Punctuation color.Color
}

// Load parses a JSON theme file from the given path.
func Load(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read theme file: %w", err)
	}

	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("invalid theme JSON: %w", err)
	}

	return &t, nil
}

// Find looks for a theme by name in user configuration first, then
// falls back to built-ins.
func Find(name string) (*Theme, error) {
	// Try user directory first
	dir, err := xdg.SubConfigDir("themes")
	if err == nil {
		path := filepath.Join(dir, name+".json")
		if t, err := Load(path); err == nil {
			return t, nil
		}
	}

	// Fallback to built-in
	return getBuiltin(name)
}

func getBuiltin(name string) (*Theme, error) {
	path := filepath.Join("builtin", name+".json")
	data, err := builtinFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("builtin theme %q not found: %w", name, err)
	}

	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("failed to parse builtin theme %q: %w", name, err)
	}

	return &t, nil
}

// Resolve converts a Theme (JSON) to a Scheme (Resolved) using the currently configured client.
func Resolve(t *Theme) (*Scheme, error) {
	c := store.Get().client
	if c == "" {
		c = ClientCLI
	}
	return ResolveFor(t, c)
}

// ResolveFor converts a Theme (JSON) to a Scheme (Resolved) for a specific client.
func ResolveFor(t *Theme, client Client) (*Scheme, error) {
	if t == nil {
		return nil, errors.New("theme: theme is nil")
	}

	chain, err := resolveThemeChain(t)
	if err != nil {
		return nil, err
	}

	merged := Theme{
		Name: t.Name,
	}
	for _, theme := range chain {
		mergeTheme(&merged, theme)
	}

	if client != "" && merged.Clients != nil {
		if ov, ok := merged.Clients[client]; ok {
			if ov.Transparent != nil {
				b := *ov.Transparent
				merged.Transparent = &b
			}
			if ov.Palette != nil {
				if merged.Palette == nil {
					merged.Palette = make(map[string]string)
				}
				maps.Copy(merged.Palette, ov.Palette)
			}
			mergeThemeColors(&merged.Colors, &ov.Colors)
		}
	}

	resolvedPalette := make(map[string]color.Color, len(merged.Palette))
	for name, hex := range merged.Palette {
		c, err := parseColor(hex)
		if err != nil {
			return nil, fmt.Errorf("invalid palette color %q: %w", name, err)
		}
		resolvedPalette[name] = c
	}

	themeType := merged.Type
	if themeType == "" {
		themeType = "dark"
	}

	s := &Scheme{
		Name:    t.Name,
		Type:    themeType,
		Palette: resolvedPalette,
	}

	if err := s.resolveTokenStruct(reflect.ValueOf(merged.Colors), reflect.ValueOf(&s.Color).Elem(), ""); err != nil {
		return nil, err
	}

	if merged.Transparent != nil && *merged.Transparent {
		s = s.WithTransparent(true)
	}

	return s, nil
}

func resolveThemeChain(t *Theme) ([]*Theme, error) {
	visited := make(map[string]bool)
	visited[t.Name] = true
	var ancestors []*Theme
	curr := t
	for curr.Extends != "" {
		parentName := strings.TrimSpace(curr.Extends)
		if visited[parentName] {
			return nil, fmt.Errorf("theme: cycle detected in extends chain: %s", parentName)
		}
		visited[parentName] = true
		parent, err := Find(parentName)
		if err != nil {
			return nil, fmt.Errorf("theme: parent theme %q not found: %w", parentName, err)
		}
		ancestors = append(ancestors, parent)
		curr = parent
	}
	slices.Reverse(ancestors)
	return append(ancestors, t), nil
}

func mergeTheme(dst *Theme, src *Theme) {
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Transparent != nil {
		b := *src.Transparent
		dst.Transparent = &b
	}
	if src.Palette != nil {
		if dst.Palette == nil {
			dst.Palette = make(map[string]string)
		}
		maps.Copy(dst.Palette, src.Palette)
	}
	mergeThemeColors(&dst.Colors, &src.Colors)
	if src.Clients != nil {
		if dst.Clients == nil {
			dst.Clients = make(map[Client]ClientOverride)
		}
		for c, srcOv := range src.Clients {
			dstOv := dst.Clients[c]
			if srcOv.Transparent != nil {
				b := *srcOv.Transparent
				dstOv.Transparent = &b
			}
			if srcOv.Palette != nil {
				if dstOv.Palette == nil {
					dstOv.Palette = make(map[string]string)
				}
				maps.Copy(dstOv.Palette, srcOv.Palette)
			}
			mergeThemeColors(&dstOv.Colors, &srcOv.Colors)
			dst.Clients[c] = dstOv
		}
	}
}

func mergeThemeColors(dst, src *ThemeColors) {
	mergeStruct(reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem())
}

func mergeStruct(dst, src reflect.Value) {
	for i := 0; i < src.NumField(); i++ {
		srcF := src.Field(i)
		dstF := dst.Field(i)
		if srcF.Kind() == reflect.String {
			if str := srcF.String(); str != "" {
				dstF.SetString(str)
			}
		} else if srcF.Kind() == reflect.Struct {
			mergeStruct(dstF, srcF)
		}
	}
}

func (s *Scheme) resolveTokenStruct(rawVal reflect.Value, resolvedVal reflect.Value, prefix string) error {
	t := rawVal.Type()
	for i := 0; i < rawVal.NumField(); i++ {
		field := t.Field(i)
		rawF := rawVal.Field(i)
		resF := resolvedVal.Field(i)

		jsonTag := field.Tag.Get("json")
		name, _, _ := strings.Cut(jsonTag, ",")
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		tokenPath := name
		if prefix != "" {
			tokenPath = prefix + "." + name
		}

		if rawF.Kind() == reflect.String {
			val := strings.TrimSpace(rawF.String())
			if val == "" {
				return fmt.Errorf("theme: missing required color token %q", tokenPath)
			}
			c, err := s.resolveColor(val)
			if err != nil {
				return fmt.Errorf("theme: invalid color for token %q (%q): %w", tokenPath, val, err)
			}
			if c != nil {
				resF.Set(reflect.ValueOf(c))
			}
		} else if rawF.Kind() == reflect.Struct {
			if err := s.resolveTokenStruct(rawF, resF, tokenPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// WithTransparent returns a copy of Scheme with the transparent setting applied.
// When transparent is true, surface Canvas, Panel, Hover, Active, and Disabled
// are cleared to nil (Elevated, Code, Selected, and Tint.* remain intact).
func (s *Scheme) WithTransparent(transparent bool) *Scheme {
	if s == nil {
		return nil
	}
	clone := *s
	clone.Transparent = transparent
	if transparent {
		clone.Color.Surface.Canvas = nil
		clone.Color.Surface.Panel = nil
		clone.Color.Surface.Hover = nil
		clone.Color.Surface.Active = nil
		clone.Color.Surface.Disabled = nil
	}
	return &clone
}

func (s *Scheme) resolveColor(val string) (color.Color, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil, errors.New("empty color value")
	}
	if strings.EqualFold(val, "none") || strings.EqualFold(val, "transparent") {
		return nil, nil
	}
	if c, ok := s.Palette[val]; ok {
		return c, nil
	}
	return parseColor(val)
}

func parseColor(hex string) (color.RGBA, error) {
	hex = strings.TrimPrefix(hex, "#")
	switch len(hex) {
	case 3:
		var r, g, b uint8
		_, err := fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
		if err != nil {
			return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
		}
		return color.RGBA{R: r * 17, G: g * 17, B: b * 17, A: 255}, nil
	case 4:
		var r, g, b, a uint8
		_, err := fmt.Sscanf(hex, "%1x%1x%1x%1x", &r, &g, &b, &a)
		if err != nil {
			return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
		}
		return color.RGBA{R: r * 17, G: g * 17, B: b * 17, A: a * 17}, nil
	case 6:
		var r, g, b uint8
		_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
		if err != nil {
			return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
		}
		return color.RGBA{R: r, G: g, B: b, A: 255}, nil
	case 8:
		var r, g, b, a uint8
		_, err := fmt.Sscanf(hex, "%02x%02x%02x%02x", &r, &g, &b, &a)
		if err != nil {
			return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
		}
		return color.RGBA{R: r, G: g, B: b, A: a}, nil
	default:
		return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
	}
}

type state struct {
	themeName   string
	scheme      *Scheme
	client      Client
	transparent bool
}

var store = kites.Create(state{
	themeName:   Default,
	scheme:      nil,
	client:      ClientCLI,
	transparent: false,
})

func init() {
	themeName := Default
	transparent := false
	if dir, err := xdg.SubConfigDir(); err == nil {
		cfgPath := filepath.Join(dir, "theme.json")
		if data, err := os.ReadFile(cfgPath); err == nil {
			var cfg struct {
				Theme       string `json:"theme"`
				Transparent bool   `json:"transparent"`
			}
			if err := json.Unmarshal(data, &cfg); err == nil {
				if cfg.Theme != "" {
					themeName = cfg.Theme
				}
				transparent = cfg.Transparent
			}
		}
	}

	t, err := getBuiltin(themeName)
	if err != nil {
		t, err = getBuiltin(Default)
		themeName = Default
	}
	if err == nil {
		s, _ := ResolveFor(t, ClientCLI)
		if transparent && s != nil {
			s = s.WithTransparent(true)
		}
		store.Set(func(st state) state {
			st.themeName = themeName
			st.scheme = s
			st.client = ClientCLI
			st.transparent = transparent
			return st
		})
	}
}

// SetClient updates the active client type and refreshes the scheme.
func SetClient(client Client) {
	store.Set(func(st state) state {
		st.client = client
		t, err := Find(st.themeName)
		if err != nil {
			t, err = getBuiltin(Default)
		}
		if err == nil {
			if s, err := ResolveFor(t, client); err == nil {
				if st.transparent {
					s = s.WithTransparent(true)
				}
				st.scheme = s
				return st
			}
		}
		return st
	})
}

// GetClient returns the currently configured client type.
func GetClient() Client {
	return store.Get().client
}

// Set updates the active global theme.
func Set(name string) error {
	t, err := Find(name)
	if err != nil {
		return fmt.Errorf("theme %q not found: %w", name, err)
	}
	client := store.Get().client
	if client == "" {
		client = ClientCLI
	}
	s, err := ResolveFor(t, client)
	if err != nil {
		return fmt.Errorf("invalid theme %q: %w", name, err)
	}
	store.Set(func(st state) state {
		st.themeName = name
		if st.transparent && s != nil {
			s = s.WithTransparent(true)
		}
		st.scheme = s
		return st
	})
	return nil
}

// SetTransparent sets whether the active global theme should be transparent.
func SetTransparent(transparent bool) {
	store.Set(func(st state) state {
		st.transparent = transparent
		t, err := Find(st.themeName)
		if err != nil {
			t, err = getBuiltin(Default)
		}
		if err == nil {
			if s, err := ResolveFor(t, st.client); err == nil {
				if transparent {
					s = s.WithTransparent(true)
				}
				st.scheme = s
				return st
			}
		}
		if st.scheme != nil {
			st.scheme = st.scheme.WithTransparent(transparent)
		}
		return st
	})
}

// IsTransparent returns whether the active global theme is configured as transparent.
func IsTransparent() bool {
	return store.Get().transparent
}

// GetName returns the current global theme name.
func GetName() string {
	return store.Get().themeName
}

// UseName returns the current theme name in a reactive hook context.
func UseName() string {
	return kites.Use(store, func(s state) string {
		return s.themeName
	})
}

// Get returns the current active scheme snapshot.
func Get() *Scheme {
	s := store.Get().scheme
	if s == nil {
		t, _ := getBuiltin(Default)
		if t != nil {
			s, _ = ResolveFor(t, ClientCLI)
		}
	}
	return s
}

// UseTheme returns the current theme from the reactive store.
func UseTheme() *Scheme {
	return kites.Use(store, func(s state) *Scheme {
		return s.scheme
	})
}

// List returns a slice of all available theme names (built-in and custom).
func List() []string {
	var themes []string

	// Read built-ins
	if entries, err := builtinFS.ReadDir("builtin"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				themes = append(themes, strings.TrimSuffix(entry.Name(), ".json"))
			}
		}
	}

	// Read user dir
	if dir, err := xdg.SubConfigDir("themes"); err == nil {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
					name := strings.TrimSuffix(entry.Name(), ".json")
					// Avoid duplicates
					exists := slices.Contains(themes, name)
					if !exists {
						themes = append(themes, name)
					}
				}
			}
		}
	}

	return themes
}
