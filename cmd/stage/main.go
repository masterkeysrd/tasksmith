package main

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/stage"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func main() {
	stg := stage.New()

	// Register global controls (toolbar items)
	stg.GlobalSelect("Theme", []string{"tokyo-night", "solarized", "github-dark"}, "tokyo-night")

	// Context-aware decorator that wraps all rendered scene nodes
	stg.WithContextDecorator(func(c *stage.Context, n kitex.Node) kitex.Node {
		themeName := c.GlobalString("Theme", "tokyo-night")
		_ = theme.Set(themeName)

		return theme.Provider(theme.Props{}, n)
	})

	// 2. Register Component scenes
	stg.Register("Button", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				label := c.Text("Label", "Themed Button")
				disabled := c.Bool("Disabled", false)
				variant := c.Select("Variant", []string{"solid", "outline", "text", "tonal"}, "solid")
				colorVal := c.Select("Color", []string{"primary", "secondary", "tertiary", "success", "info", "error"}, "primary")

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Display(style.DisplayFlex).
						JustifyContent(style.JustifyCenter).
						AlignItems(style.AlignCenter).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					components.Button(components.ButtonProps{
						Disabled: disabled,
						Variant:  components.ButtonVariant(variant),
						Color:    components.ButtonColor(colorVal),
						OnClick: func() {
							c.Log("Button clicked!")
						},
					}, kitex.Text(label)),
				)
			},
		},
	})

	stg.Register("Paper", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				variant := c.Select("Variant", []string{"default", "outlined"}, "default")
				colorVal := c.Select("Color", []string{"base", "primary", "secondary", "tertiary", "success", "info", "error"}, "base")
				paddingVal := c.Select("Padding", []string{"0", "1", "2", "3", "4"}, "1")
				padding := 1
				switch paddingVal {
				case "0":
					padding = 0
				case "1":
					padding = 1
				case "2":
					padding = 2
				case "3":
					padding = 3
				case "4":
					padding = 4
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Display(style.DisplayFlex).
						JustifyContent(style.JustifyCenter).
						AlignItems(style.AlignCenter).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					components.Paper(components.PaperProps{
						Variant: components.PaperVariant(variant),
						Color:   components.PaperColor(colorVal),
						Style:   style.S().Padding(padding),
					}, kitex.Text("This is a Paper component")),
				)
			},
		},
	})

	stg.Register("Checkbox", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				label := c.Text("Label", "Check me")
				checked := c.Bool("Checked", false)
				disabled := c.Bool("Disabled", false)
				colorVal := c.Select("Color", []string{"primary", "secondary", "tertiary", "success", "info", "error"}, "primary")

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Display(style.DisplayFlex).
						JustifyContent(style.JustifyCenter).
						AlignItems(style.AlignCenter).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					components.Checkbox(components.CheckboxProps{
						Label:    kitex.Text(label),
						Checked:  checked,
						Disabled: disabled,
						Color:    components.CheckboxColor(colorVal),
						OnChange: func(val bool) {
							c.Log("Checkbox toggled!")
						},
					}),
				)
			},
		},
	})

	stg.Register("Input", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				value := c.Text("Value", "")
				placeholder := c.Text("Placeholder", "Enter text...")
				disabled := c.Bool("Disabled", false)
				variant := c.Select("Variant", []string{"outline", "solid", "underline"}, "outline")
				colorVal := c.Select("Color", []string{"primary", "secondary", "tertiary", "success", "info", "error"}, "primary")

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Display(style.DisplayFlex).
						JustifyContent(style.JustifyCenter).
						AlignItems(style.AlignCenter).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					components.Input(components.InputProps{
						Value:       value,
						Placeholder: placeholder,
						Disabled:    disabled,
						Variant:     components.InputVariant(variant),
						Color:       components.InputColor(colorVal),
						OnChange: func(val string) {
							c.Log("Input changed: " + val)
						},
					}),
				)
			},
		},
	})

	stg.Register("Tabs", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				colorVal := c.Select("Color", []string{"base", "primary", "secondary", "tertiary", "success", "info", "error"}, "base")
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					components.Tabs(components.TabsProps{
						DefaultValue: "tab1",
						Color:        components.PaperColor(colorVal),
					},
						components.Tab(components.TabProps{
							Value: "tab1",
						}, kitex.Text("Tab 1")),
						components.Tab(components.TabProps{
							Value: "tab2",
						}, kitex.Text("Tab 2")),
						components.TabPanel(components.TabPanelProps{
							Value: "tab1",
						},
							kitex.Text("Content for Tab 1"),
						),
						components.TabPanel(components.TabPanelProps{
							Value: "tab2",
						},
							kitex.Text("Content for Tab 2"),
						),
					),
				)
			},
		},
	})

	stg.Register("Alert", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				severity := c.Select("Severity", []string{"success", "info", "warning", "error"}, "info")
				variant := c.Select("Variant", []string{"standard", "outlined"}, "standard")
				showIcon := c.Bool("Show Icon", true)
				message := c.Text("Message", "This is an alert message")

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					components.Alert(components.AlertProps{
						Severity: components.AlertSeverity(severity),
						Variant:  components.AlertVariant(variant),
						ShowIcon: showIcon,
					}, kitex.Text(message)),
				)
			},
		},
	})

	stg.Register("Accordion", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				colorVal := c.Select("Color", []string{"base", "primary", "secondary", "tertiary", "success", "info", "error"}, "base")
				variant := c.Select("Variant", []string{"default", "outlined"}, "default")
				title := c.Text("Title", "Accordion Title")

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					components.Accordion(components.AccordionProps{
						Color:   components.PaperColor(colorVal),
						Variant: components.PaperVariant(variant),
					},
						components.AccordionSummary(components.AccordionSummaryProps{}, kitex.Text(title)),
						components.AccordionDetails(components.AccordionDetailsProps{},
							kitex.Text("This is the expanded content of the accordion."),
						),
					),
				)
			},
		},
	})

	stg.Register("Card", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				colorVal := c.Select("Color", []string{"base", "primary", "secondary", "tertiary", "success", "info", "error"}, "base")
				variant := c.Select("Variant", []string{"default", "outlined"}, "default")
				title := c.Text("Title", "Card Title")
				subheader := c.Text("Subheader", "Card Subheader")

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					components.Card(components.CardProps{
						Color:   components.PaperColor(colorVal),
						Variant: components.CardVariant(variant),
					},
						components.CardHeader(components.CardHeaderProps{
							Title:     kitex.Text(title),
							Subheader: kitex.Text(subheader),
						}),
						components.CardContent(components.CardContentProps{},
							kitex.Text("This is the main content of the card."),
						),
						components.CardActions(components.CardActionsProps{},
							components.Button(components.ButtonProps{
								Variant: components.ButtonText,
							}, kitex.Text("Action")),
						),
					),
				)
			},
		},
	})

	stg.Run()
}
