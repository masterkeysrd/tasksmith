package styles

import (
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type Common struct {
	Title     style.Style
	Subtitle  style.Style
	Primary   style.Style
	Secondary style.Style
	Muted     style.Style
	Code      style.Style
	Success   style.Style
	Warning   style.Style
	Error     style.Style
	Accent    style.Style
	Row       style.Style
	Column    style.Style
	Panel     style.Style
	Selected  style.Style
}

func NewCommon(s *theme.Scheme) Common {
	text := s.Color.Text
	border := style.SingleBorder().
		Top(true).
		Bottom(false).
		Left(false).
		Right(false).
		Color(s.Color.Border.Default)

	return Common{
		Title: style.S().
			Bold(true).
			Foreground(text.Primary),
		Subtitle: style.S().
			Bold(true).
			Foreground(text.Secondary),
		Primary:   style.S().Foreground(text.Primary),
		Secondary: style.S().Foreground(text.Secondary),
		Muted:     style.S().Foreground(text.Muted),
		Code: style.S().
			Foreground(text.Primary).
			Background(s.Color.Surface.Code),
		Success: style.S().Foreground(text.Success),
		Warning: style.S().Foreground(text.Warning),
		Error:   style.S().Foreground(text.Error),
		Accent:  style.S().Foreground(text.Accent),
		Row:     style.S().FlexDirection(style.FlexRow),
		Column:  style.S().FlexDirection(style.FlexColumn),
		Panel: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
		Selected: style.S().Background(s.Color.Surface.Selected),
	}
}

func UseCommon() Common {
	return Use(NewCommon)
}
