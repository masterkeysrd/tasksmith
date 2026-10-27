package components

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
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
	GutterMinWidth  int
}

// CodeBlock renders a syntax-highlighted code block using chroma, fully styled
// using the active TUI theme.
var CodeBlock = kitex.FC("CodeBlock", func(props CodeBlockProps) kitex.Node {
	t := theme.UseTheme()

	return kitex.UseMemo(func() kitex.Node {
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
			Bold(true).
			Background(t.Color.Surface.Code).
			Foreground(t.Color.Text.Primary).
			BorderBottom(true, style.SingleBorder(), t.Color.Border.Default)

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
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Percent(0)).
			WhiteSpace(whiteSpace).
			Foreground(t.Color.Text.Secondary)
		if props.Wrap {
			codeStyle = codeStyle.
				OverflowX(style.OverflowHidden).
				OverflowWrap(style.OverflowWrapBreakWord)
		} else {
			codeStyle = codeStyle.
				OverflowX(style.OverflowAuto)
		}

		wrapperStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Percent(0)).
			WhiteSpace(whiteSpace)
		if props.Wrap {
			wrapperStyle = wrapperStyle.
				Overflow(style.OverflowHidden).
				OverflowWrap(style.OverflowWrapBreakWord)
		}

		if !props.HideHeader {
			wrapperStyle = wrapperStyle.
				MarginBottom(1).
				Background(t.Color.Surface.Code).
				Border(true, style.SingleBorder(), t.Color.Border.Default)
		} else if !props.Compact {
			wrapperStyle = wrapperStyle.Background(t.Color.Surface.Code)
		}

		wrapperStyle = wrapperStyle.Merge(props.Style)

		var contentNodes []kitex.Node

		// Fetch and coalesce lexer
		lexer := lexers.Get(lang)
		if lexer == nil {
			lexer = lexers.Fallback
		}
		lexer = chroma.Coalesce(lexer)

		// Tokenize code string
		iterator, err := lexer.Tokenise(nil, codeStr)

		if err != nil {
			fallbackStyle := style.S().WhiteSpace(whiteSpace)
			if props.Wrap {
				fallbackStyle = fallbackStyle.
					OverflowWrap(style.OverflowWrapBreakWord).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0))
			}
			contentNodes = []kitex.Node{
				kitex.Span(
					kitex.SpanProps{Style: fallbackStyle},
					kitex.Text(codeStr),
				),
			}
		} else {
			var currentType chroma.TokenType
			var currentText strings.Builder
			var hasPending bool

			flushCurrent := func() {
				if hasPending {
					tokenStyle := ResolveTokenStyle(t, currentType)
					if props.Wrap {
						tokenStyle = tokenStyle.
							WhiteSpace(style.WhiteSpacePreWrap).
							OverflowWrap(style.OverflowWrapBreakWord).
							MaxWidth(style.Percent(100)).
							MinWidth(style.Percent(0))
					}
					contentNodes = append(contentNodes, kitex.Span(
						kitex.SpanProps{Style: tokenStyle},
						kitex.Text(currentText.String()),
					))
					currentText.Reset()
					hasPending = false
				}
			}

			for tok := iterator(); tok != chroma.EOF; tok = iterator() {
				if !hasPending {
					currentType = tok.Type
					currentText.WriteString(tok.Value)
					hasPending = true
				} else if tok.Type == currentType {
					currentText.WriteString(tok.Value)
				} else {
					flushCurrent()
					currentType = tok.Type
					currentText.WriteString(tok.Value)
					hasPending = true
				}
			}
			flushCurrent()
		}

		// Line numbers rendering styles
		rowStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Percent(0))

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
		if props.GutterMinWidth > 0 {
			gutterStyle = gutterStyle.MinWidth(style.Cells(props.GutterMinWidth))
		}
		gutterStyle = gutterStyle.Foreground(t.Color.Text.Muted)

		separatorStyle := style.S().
			PaddingVertical(padY).
			Foreground(t.Color.Border.Default)

		codeBoxStyle := style.S().
			PaddingVertical(padY).
			PaddingLeft(1).
			PaddingRight(1).
			Flex(1, 1, style.Cells(0)).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Percent(0)).
			MinHeight(style.Cells(0)).
			WhiteSpace(whiteSpace).
			Foreground(t.Color.Text.Secondary)
		if props.Wrap {
			codeBoxStyle = codeBoxStyle.
				OverflowX(style.OverflowHidden).
				OverflowWrap(style.OverflowWrapBreakWord)
		} else {
			codeBoxStyle = codeBoxStyle.
				OverflowX(style.OverflowAuto)
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
	}, []any{
		t,
		props.Code,
		props.Lang,
		props.HideHeader,
		props.ShowLineNumbers,
		props.StartLine,
		props.Compact,
		props.Wrap,
		props.GutterMinWidth,
		props.Style,
	})
})

// ResolveTokenStyle maps a chroma.TokenType to an editor color dynamically from the active theme.
func ResolveTokenStyle(t *theme.Scheme, tokType chroma.TokenType) style.Style {
	themeText := t.Color.Text.Primary
	themeMuted := t.Color.Syntax.Comment
	themeKeyword := t.Color.Syntax.Keyword
	themeFunction := t.Color.Syntax.Function
	themeString := t.Color.Syntax.String
	themeNumber := t.Color.Syntax.Number
	themeType := t.Color.Syntax.Type
	themeOperator := t.Color.Syntax.Operator
	themeVariable := t.Color.Syntax.Variable
	themePunctuation := t.Color.Syntax.Punctuation

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
		case chroma.NameVariable, chroma.NameVariableClass, chroma.NameVariableGlobal, chroma.NameVariableInstance:
			styled = styled.Foreground(themeVariable)
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
		styled = styled.Foreground(themePunctuation)
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
