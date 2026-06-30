package components

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// CodeBlockProps defines properties for the CodeBlock rendering component.
type CodeBlockProps struct {
	Code            string
	Lang            string
	Style           style.Style
	HideHeader      bool
	ShowLineNumbers bool
	StartLine       int
	Compact         bool
	Wrap            bool
}

// CodeBlock renders a syntax-highlighted code block using chroma, fully styled
// using the active TUI theme.
var CodeBlock = kitex.FC("CodeBlock", func(props CodeBlockProps) kitex.Node {
	t := theme.UseTheme()
	codeStr := strings.ReplaceAll(props.Code, "\t", "    ")
	lang := props.Lang

	if strings.TrimSpace(codeStr) == "" {
		return nil
	}
	if lang == "" {
		lang = "code"
	}

	titleStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		JustifyContent(style.JustifyBetween).
		PaddingHorizontal(1).
		BorderBottom(true, style.SingleBorder()).
		Bold(true)
	if t != nil {
		titleStyle = titleStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(t.Color.Text.Primary).
			BorderBottom(true, style.SingleBorder(), t.Color.Border.Primary)
	}

	padCode := 1
	if props.Compact {
		padCode = 0
	}
	whiteSpace := style.WhiteSpacePre
	if props.Wrap {
		whiteSpace = style.WhiteSpacePreWrap
	}
	codeStyle := style.S().
		Padding(padCode).
		WhiteSpace(whiteSpace).
		OverflowX(style.OverflowAuto)
	if t != nil {
		codeStyle = codeStyle.Foreground(t.Color.Text.Secondary)
	}
	if props.Wrap {
		codeStyle = codeStyle.OverflowWrap(style.OverflowWrapBreakWord)
	}

	wrapperStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MinWidth(style.Percent(0)).
		WhiteSpace(whiteSpace)
	if props.Wrap {
		wrapperStyle = wrapperStyle.OverflowWrap(style.OverflowWrapBreakWord)
	}

	if !props.HideHeader {
		wrapperStyle = wrapperStyle.
			MarginBottom(1).
			Border(style.SingleBorder())
		if t != nil {
			wrapperStyle = wrapperStyle.
				Background(t.Color.Surface.BaseHover).
				Border(true, style.SingleBorder(), t.Color.Border.Primary)
		}
	} else if t != nil && !props.Compact {
		wrapperStyle = wrapperStyle.Background(t.Color.Surface.BaseHover)
	}

	wrapperStyle = wrapperStyle.Merge(props.Style)

	// Fetch and coalesce lexer
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	// Tokenize code string
	iterator, err := lexer.Tokenise(nil, codeStr)
	var contentNodes []kitex.Node
	if err != nil {
		contentNodes = []kitex.Node{
			kitex.Span(
				kitex.SpanProps{Style: style.S().WhiteSpace(whiteSpace)},
				kitex.Text(codeStr),
			),
		}
	} else {
		for tok := iterator(); tok != chroma.EOF; tok = iterator() {
			tokenStyle := ResolveTokenStyle(t, tok.Type)
			if props.Wrap {
				tokenStyle = tokenStyle.WhiteSpace(style.WhiteSpacePreWrap).
					OverflowWrap(style.OverflowWrapBreakWord)
			}
			contentNodes = append(contentNodes, kitex.Span(
				kitex.SpanProps{Style: tokenStyle},
				kitex.Text(tok.Value),
			))
		}
	}

	// Line numbers rendering styles
	rowStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Width(style.Percent(100))

	padY := 1
	if props.Compact {
		padY = 0
	}

	gutterStyle := style.S().
		PaddingVertical(padY).
		PaddingLeft(1).
		PaddingRight(1).
		TextAlign(style.TextAlignRight).
		WhiteSpace(style.WhiteSpacePre)
	if t != nil {
		gutterStyle = gutterStyle.Foreground(t.Color.Text.Tertiary)
	}

	separatorStyle := style.S().
		PaddingVertical(padY)
	if t != nil {
		separatorStyle = separatorStyle.Foreground(t.Color.Border.Primary)
	}

	codeBoxStyle := style.S().
		PaddingVertical(padY).
		PaddingLeft(1).
		PaddingRight(1).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		WhiteSpace(whiteSpace).
		OverflowX(style.OverflowAuto)
	if t != nil {
		codeBoxStyle = codeBoxStyle.Foreground(t.Color.Text.Secondary)
	}
	if props.Wrap {
		codeBoxStyle = codeBoxStyle.OverflowWrap(style.OverflowWrapBreakWord)
	}

	var gutterText string
	var separatorText string
	if props.ShowLineNumbers {
		start := props.StartLine
		if start <= 0 {
			start = 1
		}
		lineCount := 0
		if codeStr != "" {
			lineCount = strings.Count(codeStr, "\n") + 1
		}
		var gutterBuilder strings.Builder
		var sepBuilder strings.Builder
		for i := 0; i < lineCount; i++ {
			if i > 0 {
				gutterBuilder.WriteByte('\n')
				sepBuilder.WriteByte('\n')
			}
			fmt.Fprintf(&gutterBuilder, "%d", start+i)
			sepBuilder.WriteString("│")
		}
		gutterText = gutterBuilder.String()
		separatorText = sepBuilder.String()
	}

	var codeContainer kitex.Node
	if props.ShowLineNumbers {
		codeContainer = kitex.Box(kitex.BoxProps{Style: rowStyle},
			kitex.Box(kitex.BoxProps{Style: gutterStyle},
				kitex.Text(gutterText),
			),
			kitex.Box(kitex.BoxProps{Style: separatorStyle},
				kitex.Text(separatorText),
			),
			kitex.Box(kitex.BoxProps{Style: codeBoxStyle},
				contentNodes...,
			),
		)
	} else {
		codeContainer = kitex.Box(kitex.BoxProps{Style: codeStyle},
			contentNodes...,
		)
	}

	return kitex.Box(kitex.BoxProps{Style: wrapperStyle},
		kitex.If(!props.HideHeader, func() kitex.Node {
			return kitex.Box(kitex.BoxProps{Style: titleStyle},
				kitex.Span(kitex.SpanProps{}, kitex.Text(strings.ToUpper(lang))),
				kitex.Span(kitex.SpanProps{}, kitex.Text("READONLY")),
			)
		}),
		codeContainer,
	)
})

func getThemeColor(t *theme.Scheme, key string, fallback color.Color) color.Color {
	if t != nil && t.Palette != nil {
		if c, ok := t.Palette[key]; ok {
			return c
		}
	}
	return fallback
}

// ResolveTokenStyle maps a chroma.TokenType to an editor color dynamically from the active theme.
func ResolveTokenStyle(t *theme.Scheme, tokType chroma.TokenType) style.Style {
	// Fallbacks match the One Dark color palette
	themeText := color.Color(color.RGBA{R: 171, G: 178, B: 191, A: 255})
	themeMuted := color.Color(color.RGBA{R: 92, G: 99, B: 112, A: 255})
	themeKeyword := color.Color(color.RGBA{R: 198, G: 120, B: 221, A: 255})
	themeFunction := color.Color(color.RGBA{R: 97, G: 175, B: 239, A: 255})
	themeString := color.Color(color.RGBA{R: 152, G: 195, B: 121, A: 255})
	themeNumber := color.Color(color.RGBA{R: 209, G: 154, B: 102, A: 255})
	themeType := color.Color(color.RGBA{R: 229, G: 192, B: 123, A: 255})
	themeOperator := color.Color(color.RGBA{R: 86, G: 182, B: 194, A: 255})

	if t != nil {
		themeText = t.Color.Text.Primary
		themeMuted = t.Color.Text.Tertiary
		themeKeyword = t.Color.Text.Purple
		themeFunction = getThemeColor(t, "blue", getThemeColor(t, "cyan", t.Color.Text.Magenta))
		themeString = getThemeColor(t, "green", t.Color.Text.Primary)
		themeNumber = getThemeColor(t, "orange", getThemeColor(t, "yellow", t.Color.Text.Secondary))
		themeType = getThemeColor(t, "yellow", t.Color.Text.Primary)
		themeOperator = getThemeColor(t, "cyan", t.Color.Text.Secondary)
	}

	styled := style.S().WhiteSpace(style.WhiteSpacePre)

	if tokType.InCategory(chroma.Comment) {
		styled = styled.Foreground(themeMuted)
	} else if tokType.InCategory(chroma.Keyword) {
		if tokType == chroma.KeywordType {
			styled = styled.Foreground(themeType)
		} else {
			styled = styled.Foreground(themeKeyword)
		}
		if tokType == chroma.Keyword || tokType == chroma.KeywordNamespace {
			styled = styled.Bold(true)
		}
	} else if tokType.InCategory(chroma.Name) {
		switch tokType {
		case chroma.NameFunction, chroma.NameBuiltin:
			styled = styled.Foreground(themeFunction)
		case chroma.NameClass:
			styled = styled.Foreground(themeType)
		case chroma.NameTag:
			styled = styled.Foreground(themeKeyword)
		case chroma.NameAttribute:
			styled = styled.Foreground(themeNumber)
		default:
			styled = styled.Foreground(themeText)
		}
	} else if tokType.InCategory(chroma.Literal) {
		if tokType.InSubCategory(chroma.LiteralString) {
			styled = styled.Foreground(themeString)
		} else if tokType.InSubCategory(chroma.LiteralNumber) {
			styled = styled.Foreground(themeNumber)
		} else {
			styled = styled.Foreground(themeText)
		}
	} else if tokType.InCategory(chroma.Operator) {
		styled = styled.Foreground(themeOperator)
	} else if tokType.InCategory(chroma.Punctuation) {
		styled = styled.Foreground(themeText)
	} else if tokType.InCategory(chroma.Generic) {
		switch tokType {
		case chroma.GenericInserted:
			styled = styled.Foreground(themeString)
		case chroma.GenericDeleted:
			styled = styled.Foreground(themeNumber)
		case chroma.GenericStrong:
			styled = styled.Foreground(themeText).Bold(true)
		case chroma.GenericEmph:
			styled = styled.Foreground(themeText).Italic(true)
		default:
			styled = styled.Foreground(themeText)
		}
	} else {
		styled = styled.Foreground(themeText)
	}

	return styled
}
