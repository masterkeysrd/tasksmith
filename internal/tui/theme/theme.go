package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// Default is the name of the default theme.
const Default = "default"

//go:embed builtin/*.json
var builtinFS embed.FS

// Theme represents the JSON structure of a theme.
type Theme struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Palette map[string]string `json:"palette,omitempty"`
	Colors  ThemeColors       `json:"colors"`
}

// ThemeColors represents the JSON structure for theme colors.
type ThemeColors struct {
	Icon    ThemeIconColor    `json:"icon"`
	Text    ThemeTextColor    `json:"text"`
	Border  ThemeBorderColor  `json:"border"`
	Surface ThemeSurfaceColor `json:"surface"`
}

type ThemeIconColor struct {
	Primary string `json:"primary"`
}
type ThemeTextColor struct {
	Primary        string `json:"primary"`
	InversePrimary string `json:"inverse_primary"`
	Secondary      string `json:"secondary"`
	Tertiary       string `json:"tertiary"`
	Purple         string `json:"purple"`
	Magenta        string `json:"magenta"`
	Error          string `json:"error"`
}

type ThemeSurfaceColor struct {
	Base         string `json:"base"`
	BaseHover    string `json:"base_hover"`
	BasePressed  string `json:"base_pressed"`
	BaseFocus    string `json:"base_focus"`
	BaseDisabled string `json:"base_disabled"`

	Primary         string `json:"primary"`
	PrimaryHover    string `json:"primary_hover"`
	PrimaryPressed  string `json:"primary_pressed"`
	PrimaryFocus    string `json:"primary_focus"`
	PrimaryDisabled string `json:"primary_disabled"`

	Secondary         string `json:"secondary"`
	SecondaryHover    string `json:"secondary_hover"`
	SecondaryPressed  string `json:"secondary_pressed"`
	SecondaryFocus    string `json:"secondary_focus"`
	SecondaryDisabled string `json:"secondary_disabled"`

	Tertiary         string `json:"tertiary"`
	TertiaryHover    string `json:"tertiary_hover"`
	TertiaryPressed  string `json:"tertiary_pressed"`
	TertiaryFocus    string `json:"tertiary_focus"`
	TertiaryDisabled string `json:"tertiary_disabled"`

	Success         string `json:"success"`
	SuccessHover    string `json:"success_hover"`
	SuccessPressed  string `json:"success_pressed"`
	SuccessFocus    string `json:"success_focus"`
	SuccessDisabled string `json:"success_disabled"`

	Info         string `json:"info"`
	InfoHover    string `json:"info_hover"`
	InfoPressed  string `json:"info_pressed"`
	InfoFocus    string `json:"info_focus"`
	InfoDisabled string `json:"info_disabled"`

	Error         string `json:"error"`
	ErrorHover    string `json:"error_hover"`
	ErrorPressed  string `json:"error_pressed"`
	ErrorFocus    string `json:"error_focus"`
	ErrorDisabled string `json:"error_disabled"`
}

type ThemeBorderColor struct {
	Primary string `json:"primary"`
}

// Scheme represents the resolved theme values.
type Scheme struct {
	Name    string
	Type    string
	Palette map[string]color.Color
	Color   Color
}

// Color represents the resolved theme colors.
type Color struct {
	Icon    IconColor
	Text    TextColor
	Border  BorderColor
	Surface SurfaceColor
}

type IconColor struct {
	Primary color.Color
}
type TextColor struct {
	Primary        color.Color
	InversePrimary color.Color
	Secondary      color.Color
	Tertiary       color.Color
	Purple         color.Color
	Magenta        color.Color
	Error          color.Color
}

type SurfaceColor struct {
	Base         color.Color
	BaseHover    color.Color
	BasePressed  color.Color
	BaseFocus    color.Color
	BaseDisabled color.Color

	Primary         color.Color
	PrimaryHover    color.Color
	PrimaryPressed  color.Color
	PrimaryFocus    color.Color
	PrimaryDisabled color.Color

	Secondary         color.Color
	SecondaryHover    color.Color
	SecondaryPressed  color.Color
	SecondaryFocus    color.Color
	SecondaryDisabled color.Color

	Tertiary         color.Color
	TertiaryHover    color.Color
	TertiaryPressed  color.Color
	TertiaryFocus    color.Color
	TertiaryDisabled color.Color

	Success         color.Color
	SuccessHover    color.Color
	SuccessPressed  color.Color
	SuccessFocus    color.Color
	SuccessDisabled color.Color

	Info         color.Color
	InfoHover    color.Color
	InfoPressed  color.Color
	InfoFocus    color.Color
	InfoDisabled color.Color

	Error         color.Color
	ErrorHover    color.Color
	ErrorPressed  color.Color
	ErrorFocus    color.Color
	ErrorDisabled color.Color
}

type BorderColor struct {
	Primary color.Color
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

// Resolve converts a Theme (JSON) to a Scheme (Resolved).
func Resolve(t *Theme) (*Scheme, error) {
	s := &Scheme{
		Name:    t.Name,
		Type:    t.Type,
		Palette: make(map[string]color.Color),
	}

	// Resolve palette
	for name, hex := range t.Palette {
		c, err := parseColor(hex)
		if err != nil {
			return nil, fmt.Errorf("invalid palette color %q: %w", name, err)
		}
		s.Palette[name] = c
	}

	// Resolve colors
	var err error
	if s.Color.Icon.Primary, err = s.resolveColor(t.Colors.Icon.Primary); err != nil {
		return nil, fmt.Errorf("failed to resolve icon primary color: %w", err)
	}

	if s.Color.Text.Primary, err = s.resolveColor(t.Colors.Text.Primary); err != nil {
		return nil, fmt.Errorf("failed to resolve text primary color: %w", err)
	}
	if s.Color.Text.InversePrimary, err = s.resolveColor(t.Colors.Text.InversePrimary); err != nil {
		return nil, fmt.Errorf("failed to resolve text inverse primary color: %w", err)
	}
	if s.Color.Text.Secondary, err = s.resolveColor(t.Colors.Text.Secondary); err != nil {
		return nil, fmt.Errorf("failed to resolve text secondary color: %w", err)
	}
	if s.Color.Text.Tertiary, err = s.resolveColor(t.Colors.Text.Tertiary); err != nil {
		return nil, fmt.Errorf("failed to resolve text tertiary color: %w", err)
	}
	if s.Color.Text.Purple, err = s.resolveColor(t.Colors.Text.Purple); err != nil {
		return nil, fmt.Errorf("failed to resolve text purple color: %w", err)
	}
	if s.Color.Text.Magenta, err = s.resolveColor(t.Colors.Text.Magenta); err != nil {
		return nil, fmt.Errorf("failed to resolve text magenta color: %w", err)
	}
	if s.Color.Text.Error, err = s.resolveColor(t.Colors.Text.Error); err != nil {
		return nil, fmt.Errorf("failed to resolve text error color: %w", err)
	}

	if s.Color.Border.Primary, err = s.resolveColor(t.Colors.Border.Primary); err != nil {
		return nil, fmt.Errorf("failed to resolve border primary color: %w", err)
	}

	// Base Surface
	if s.Color.Surface.Base, err = s.resolveColor(t.Colors.Surface.Base); err != nil {
		return nil, fmt.Errorf("failed to resolve surface base color: %w", err)
	}
	if s.Color.Surface.BaseHover, err = s.resolveColor(t.Colors.Surface.BaseHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface base hover color: %w", err)
	}
	if s.Color.Surface.BasePressed, err = s.resolveColor(t.Colors.Surface.BasePressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface base pressed color: %w", err)
	}
	if s.Color.Surface.BaseFocus, err = s.resolveColor(t.Colors.Surface.BaseFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface base focus color: %w", err)
	}
	if s.Color.Surface.BaseDisabled, err = s.resolveColor(t.Colors.Surface.BaseDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface base disabled color: %w", err)
	}

	// Primary Surface
	if s.Color.Surface.Primary, err = s.resolveColor(t.Colors.Surface.Primary); err != nil {
		return nil, fmt.Errorf("failed to resolve surface primary color: %w", err)
	}
	if s.Color.Surface.PrimaryHover, err = s.resolveColor(t.Colors.Surface.PrimaryHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface primary hover color: %w", err)
	}
	if s.Color.Surface.PrimaryPressed, err = s.resolveColor(t.Colors.Surface.PrimaryPressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface primary pressed color: %w", err)
	}
	if s.Color.Surface.PrimaryFocus, err = s.resolveColor(t.Colors.Surface.PrimaryFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface primary focus color: %w", err)
	}
	if s.Color.Surface.PrimaryDisabled, err = s.resolveColor(t.Colors.Surface.PrimaryDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface primary disabled color: %w", err)
	}

	// Secondary Surface
	if s.Color.Surface.Secondary, err = s.resolveColor(t.Colors.Surface.Secondary); err != nil {
		return nil, fmt.Errorf("failed to resolve surface secondary color: %w", err)
	}
	if s.Color.Surface.SecondaryHover, err = s.resolveColor(t.Colors.Surface.SecondaryHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface secondary hover color: %w", err)
	}
	if s.Color.Surface.SecondaryPressed, err = s.resolveColor(t.Colors.Surface.SecondaryPressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface secondary pressed color: %w", err)
	}
	if s.Color.Surface.SecondaryFocus, err = s.resolveColor(t.Colors.Surface.SecondaryFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface secondary focus color: %w", err)
	}
	if s.Color.Surface.SecondaryDisabled, err = s.resolveColor(t.Colors.Surface.SecondaryDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface secondary disabled color: %w", err)
	}

	// Tertiary Surface
	if s.Color.Surface.Tertiary, err = s.resolveColor(t.Colors.Surface.Tertiary); err != nil {
		return nil, fmt.Errorf("failed to resolve surface tertiary color: %w", err)
	}
	if s.Color.Surface.TertiaryHover, err = s.resolveColor(t.Colors.Surface.TertiaryHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface tertiary hover color: %w", err)
	}
	if s.Color.Surface.TertiaryPressed, err = s.resolveColor(t.Colors.Surface.TertiaryPressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface tertiary pressed color: %w", err)
	}
	if s.Color.Surface.TertiaryFocus, err = s.resolveColor(t.Colors.Surface.TertiaryFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface tertiary focus color: %w", err)
	}
	if s.Color.Surface.TertiaryDisabled, err = s.resolveColor(t.Colors.Surface.TertiaryDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface tertiary disabled color: %w", err)
	}

	// Success Surface
	if s.Color.Surface.Success, err = s.resolveColor(t.Colors.Surface.Success); err != nil {
		return nil, fmt.Errorf("failed to resolve surface success color: %w", err)
	}
	if s.Color.Surface.SuccessHover, err = s.resolveColor(t.Colors.Surface.SuccessHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface success hover color: %w", err)
	}
	if s.Color.Surface.SuccessPressed, err = s.resolveColor(t.Colors.Surface.SuccessPressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface success pressed color: %w", err)
	}
	if s.Color.Surface.SuccessFocus, err = s.resolveColor(t.Colors.Surface.SuccessFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface success focus color: %w", err)
	}
	if s.Color.Surface.SuccessDisabled, err = s.resolveColor(t.Colors.Surface.SuccessDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface success disabled color: %w", err)
	}

	// Info Surface
	if s.Color.Surface.Info, err = s.resolveColor(t.Colors.Surface.Info); err != nil {
		return nil, fmt.Errorf("failed to resolve surface info color: %w", err)
	}
	if s.Color.Surface.InfoHover, err = s.resolveColor(t.Colors.Surface.InfoHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface info hover color: %w", err)
	}
	if s.Color.Surface.InfoPressed, err = s.resolveColor(t.Colors.Surface.InfoPressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface info pressed color: %w", err)
	}
	if s.Color.Surface.InfoFocus, err = s.resolveColor(t.Colors.Surface.InfoFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface info focus color: %w", err)
	}
	if s.Color.Surface.InfoDisabled, err = s.resolveColor(t.Colors.Surface.InfoDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface info disabled color: %w", err)
	}

	// Error Surface
	if s.Color.Surface.Error, err = s.resolveColor(t.Colors.Surface.Error); err != nil {
		return nil, fmt.Errorf("failed to resolve surface error color: %w", err)
	}
	if s.Color.Surface.ErrorHover, err = s.resolveColor(t.Colors.Surface.ErrorHover); err != nil {
		return nil, fmt.Errorf("failed to resolve surface error hover color: %w", err)
	}
	if s.Color.Surface.ErrorPressed, err = s.resolveColor(t.Colors.Surface.ErrorPressed); err != nil {
		return nil, fmt.Errorf("failed to resolve surface error pressed color: %w", err)
	}
	if s.Color.Surface.ErrorFocus, err = s.resolveColor(t.Colors.Surface.ErrorFocus); err != nil {
		return nil, fmt.Errorf("failed to resolve surface error focus color: %w", err)
	}
	if s.Color.Surface.ErrorDisabled, err = s.resolveColor(t.Colors.Surface.ErrorDisabled); err != nil {
		return nil, fmt.Errorf("failed to resolve surface error disabled color: %w", err)
	}

	return s, nil
}

func (s *Scheme) resolveColor(val string) (color.Color, error) {
	if val == "" {
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

var themeCtx = kitex.CreateContext[*Scheme](nil)

type state struct {
	themeName string
	scheme    *Scheme
}

var store = kites.Create(state{
	themeName: Default,
	scheme:    nil,
})

func init() {
	themeName := Default
	if dir, err := xdg.SubConfigDir(); err == nil {
		cfgPath := filepath.Join(dir, "theme.json")
		if data, err := os.ReadFile(cfgPath); err == nil {
			var cfg struct {
				Theme string `json:"theme"`
			}
			if err := json.Unmarshal(data, &cfg); err == nil && cfg.Theme != "" {
				themeName = cfg.Theme
			}
		}
	}

	t, err := getBuiltin(themeName)
	if err != nil {
		t, err = getBuiltin(Default)
		themeName = Default
	}
	if err == nil {
		s, _ := Resolve(t)
		store.Set(func(st state) state {
			st.themeName = themeName
			st.scheme = s
			return st
		})
	}
}

// Set updates the active global theme.
func Set(name string) error {
	t, err := Find(name)
	if err != nil {
		return fmt.Errorf("theme %q not found: %w", name, err)
	}
	s, err := Resolve(t)
	if err != nil {
		return fmt.Errorf("invalid theme %q: %w", name, err)
	}
	store.Set(func(st state) state {
		st.themeName = name
		st.scheme = s
		return st
	})
	return nil
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

// Props defines the properties for the theme Provider.
type Props struct {
	Theme *Scheme
}

// Provider is a Kite component that provides the theme to its children.
func Provider(props Props, children ...kitex.Node) kitex.Node {
	var currentScheme *Scheme
	if props.Theme != nil {
		currentScheme = props.Theme
	} else {
		currentScheme = kites.Use(store, func(s state) *Scheme {
			return s.scheme
		})
	}
	return themeCtx.Provider(currentScheme, children...)
}

// UseTheme returns the current theme from the context.
func UseTheme() *Scheme {
	return kitex.UseContext(themeCtx)
}
