package titlebar

import (
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// Props defines the properties for the TitleBar component.
type Props struct {
	// WorkspaceName is the name of the active workspace.
	WorkspaceName string
	// IsSidebarOpen indicates whether the explorer sidebar is open.
	IsSidebarOpen bool
	// OnOpenSetupWizard is called when the Setup Wizard button is clicked.
	OnOpenSetupWizard func()
	// OnToggleSidebar is called when the Explorer button is clicked.
	OnToggleSidebar func()
}

// View is the application title bar component.
var View = kitex.FC("TitleBar", func(props Props) kitex.Node {
	t := theme.UseTheme()

	// Live clock: tick every second.
	now, setNow := kitex.UseState(time.Now())
	kitex.UseEffectCleanup(func() func() {
		ticker := time.NewTicker(time.Second)
		go func() {
			for tick := range ticker.C {
				setNow(tick)
			}
		}()
		return func() { ticker.Stop() }
	}, []any{})

	clock := now().Format("15:04")

	// ── Styles ────────────────────────────────────────────────────────────

	barStyle := style.S().
		Width(style.Percent(100)).
		Height(style.Cells(1)).
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Background(t.Color.Surface.BaseHover).
		Foreground(t.Color.Text.Tertiary).
		PaddingHorizontal(2).
		Border(style.SingleBorder().Top(false).Left(false).Right(false).Bottom(true).Color(t.Color.Surface.BaseFocus))

	leftStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(4)

	rightStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(3)

	brandStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(2)

	wsStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(2)

	authStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(2).
		Foreground(t.Color.Text.Purple)

	// Explorer button changes color when sidebar is open.
	explorerFg := t.Color.Text.Tertiary
	if props.IsSidebarOpen {
		explorerFg = t.Color.Surface.Primary
	}

	wsName := props.WorkspaceName
	if wsName == "" {
		wsName = "—"
	}

	// ── Layout ────────────────────────────────────────────────────────────

	return kitex.Box(kitex.BoxProps{Style: barStyle},
		// Left: branding + workspace
		kitex.Box(kitex.BoxProps{Style: leftStyle},
			kitex.Box(kitex.BoxProps{Style: brandStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Surface.Success),
				}, icon.Robot),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Text.Primary).Bold(true),
				}, kitex.Text("TASKSMITH")),
			),
			kitex.Box(kitex.BoxProps{Style: wsStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Text.Tertiary),
				}, kitex.Text("WORKSPACE")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Text.Secondary),
				}, kitex.Text(wsName)),
			),
		),

		// Right: buttons + indicators
		kitex.Box(kitex.BoxProps{Style: rightStyle},
			// Setup Wizard
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Style: style.S().
					Foreground(t.Color.Surface.Tertiary).
					PaddingHorizontal(0),
				HoverStyle: style.S().Foreground(t.Color.Text.Primary),
				StartIcon:  icon.Cog,
				OnClick:    props.OnOpenSetupWizard,
			}, kitex.Text("SETUP WIZARD")),

			// Explorer toggle
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Style: style.S().
					Foreground(explorerFg).
					PaddingHorizontal(0),
				HoverStyle: style.S().Foreground(t.Color.Text.Secondary),
				StartIcon:  icon.Folder,
				OnClick:    props.OnToggleSidebar,
			},
				kitex.Text("EXPLORER"),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Text.Tertiary),
				}, kitex.Text("[CTRL-B]")),
			),

			// Separator │
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(t.Color.Border.Primary),
			}, kitex.Text("│")),

			// Auth indicator
			kitex.Box(kitex.BoxProps{Style: authStyle},
				icon.Checkmark,
				kitex.Text(" AUTH_ENHANCED"),
			),

			// Clock
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(t.Color.Text.Tertiary),
			}, kitex.Text(clock)),
		),
	)
})
