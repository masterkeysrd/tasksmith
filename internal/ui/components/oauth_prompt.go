package components

import (
	"cmp"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/clipboard"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type OAuthPromptProps struct {
	Title       string
	AuthURL     string
	Status      string
	BrowserOnly bool
	EscLabel    string
	OnCopy      func()
	OnSubmit    func(callbackURL string)
	OnClose     func()
}

type oauthPromptStyles struct {
	styles.Common
	Body  style.Style
	URL   style.Style
	Input style.Style
}

func newOAuthPromptStyles(s *theme.Scheme) oauthPromptStyles {
	common := styles.NewCommon(s)
	return oauthPromptStyles{
		Common: common,
		Body: common.Column.
			Gap(1, 0),
		URL: common.Accent.
			WhiteSpace(style.WhiteSpacePreWrap).
			OverflowWrap(style.OverflowWrapAnywhere),
		Input: style.S().
			Flex(1).
			MinWidth(style.Cells(0)),
	}
}

var OAuthPrompt = trail.FC("OAuthPrompt", func(props OAuthPromptProps) trail.Node {
	st := styles.Use(newOAuthPromptStyles)
	doc := kitex.UseDocument()
	input := kitex.UseRef[dom.Element](nil)
	value, setValue := kitex.UseState("")
	copied, setCopied := kitex.UseState(false)
	trail.UseFocus(input, trail.FocusOptions{AutoFocus: true, IsActive: !props.BrowserOnly})
	kitex.UseEffect(func() { setCopied(false) }, []any{props.Status, props.AuthURL})

	bindings := UseKeyMap([]KeyAction{
		{
			Keys:     []string{"ctrl+y"},
			Hint:     "ctrl+y",
			Label:    "Copy URL",
			Disabled: props.BrowserOnly || strings.TrimSpace(props.AuthURL) == "",
			OnPress: func() {
				if clipboard.WriteText(doc(), props.AuthURL) {
					setCopied(true)
					if props.OnCopy != nil {
						props.OnCopy()
					}
				}
			},
		},
		{
			Keys:     []string{"enter"},
			Label:    "Submit pasted URL",
			Disabled: props.BrowserOnly,
			OnPress: func() {
				if pasted := strings.TrimSpace(value()); pasted != "" && props.OnSubmit != nil {
					setValue("")
					props.OnSubmit(pasted)
				}
			},
		},
		{
			Keys:    []string{"esc", "escape"},
			Hint:    "esc",
			Label:   cmp.Or(strings.TrimSpace(props.EscLabel), "Back"),
			OnPress: props.OnClose,
		},
	}, true)

	line := func(face style.Style, text string) trail.Node {
		return trail.Box(trail.BoxProps{Style: st.Row},
			trail.Span(trail.SpanProps{Style: face}, trail.Text(text)),
		)
	}

	return Panel(PanelProps{Title: props.Title, Bindings: bindings},
		trail.Box(trail.BoxProps{Style: st.Body},
			line(st.Secondary, "✻ A browser window will open for authentication"),
			kitex.If(props.Status != "", func() trail.Node {
				return line(st.Warning, props.Status)
			}),
			kitex.If(!props.BrowserOnly, func() trail.Node {
				return kitex.Fragment(
					line(st.Secondary, "If your browser doesn't open automatically, copy this URL manually:"),
					line(st.URL, cmp.Or(props.AuthURL, "Waiting for the authorization URL…")),
					kitex.If(copied(), func() trail.Node {
						return line(st.Secondary, "Copied authorization URL to clipboard.")
					}),
					line(st.Secondary, "If the redirect page shows a connection error, paste the URL from your browser's address bar:"),
					trail.Box(trail.BoxProps{Style: st.Row},
						trail.Span(trail.SpanProps{Style: st.Secondary}, trail.Text("URL > ")),
						kitex.Input(kitex.InputProps{
							Ref:   input,
							Style: st.Input,
							Value: value(),
							OnChange: func(e event.Event) {
								if input, ok := e.(*event.InputEvent); ok {
									setValue(input.Value)
								}
							},
						}),
					),
					line(st.Muted, "Return here after authenticating in your browser. Press Esc to go back."),
				)
			}),
		),
	)
})
