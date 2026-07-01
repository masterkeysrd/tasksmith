package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/stage"
	kitelog "github.com/masterkeysrd/kite/log"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/chat"
)

func main() {
	// Initialize logger — write to a temp file so the stage TUI is not polluted.
	logFile, err := os.CreateTemp("", "tasksmith-stage-*.log")
	if err == nil {
		log.SetDefault(log.New(logFile, log.LevelDebug))
		kitelog.SetLogger(slog.Default())
	}

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

	stg.Register("QueuedBubble", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				text := c.Text("Message", "Can you add dark mode support to the settings panel?")
				isOptimistic := c.Bool("Optimistic", false)

				id := "msg-001"
				if isOptimistic {
					id = "opt_msg-001"
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					chat.QueuedBubble(chat.QueuedBubbleProps{
						ID:   id,
						Text: text,
						OnEdit: func(msgID string) {
							c.Log("Edit clicked: " + msgID)
						},
						OnRemove: func(msgID string) {
							c.Log("Remove clicked: " + msgID)
						},
					}),
				)
			},
		},
		{
			Name: "Multiple",
			Render: func(c *stage.Context) kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(2).
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Width(style.Percent(100)).
						Height(style.Percent(100)),
				},
					chat.QueuedBubble(chat.QueuedBubbleProps{
						ID:       "msg-001",
						Text:     "Can you add dark mode support to the settings panel?",
						OnEdit:   func(id string) { c.Log("Edit: " + id) },
						OnRemove: func(id string) { c.Log("Remove: " + id) },
					}),
					chat.QueuedBubble(chat.QueuedBubbleProps{
						ID:       "msg-002",
						Text:     "Also update the README with the new instructions.",
						OnEdit:   func(id string) { c.Log("Edit: " + id) },
						OnRemove: func(id string) { c.Log("Remove: " + id) },
					}),
					chat.QueuedBubble(chat.QueuedBubbleProps{
						ID:   "opt_msg-003",
						Text: "This one is optimistic — no Edit/Remove actions.",
					}),
				)
			},
		},
	})

	stg.Register("DeniedToolWidget", []stage.Scene{
		{
			Name: "Default",
			Render: func(c *stage.Context) kitex.Node {
				toolName := c.Text("Tool Name", "bash")
				denyReason := c.Text("Deny Reason", "User rejected the action")

				tc := &message.ToolCall{Name: toolName}
				tm := &message.Tool{}
				tm.SetMetadata(map[string]any{"deny_reason": denyReason})

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.DeniedToolWidget(chat.ToolExecutionProps{
						ToolCall:    tc,
						ToolMessage: tm,
					}),
				)
			},
		},
		{
			Name: "No Reason",
			Render: func(c *stage.Context) kitex.Node {
				tc := &message.ToolCall{Name: "write"}
				tm := &message.Tool{}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.DeniedToolWidget(chat.ToolExecutionProps{
						ToolCall:    tc,
						ToolMessage: tm,
					}),
				)
			},
		},
	})

	stg.Register("GenericToolWidget", []stage.Scene{
		{
			Name: "Running",
			Render: func(c *stage.Context) kitex.Node {
				toolName := c.Text("Tool Name", "my_custom_tool")

				tc := &message.ToolCall{
					Name: toolName,
					Args: map[string]any{"param": "value"},
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.GenericToolWidget(chat.ToolExecutionProps{
						ToolCall: tc,
					}),
				)
			},
		},
		{
			Name: "Success",
			Render: func(c *stage.Context) kitex.Node {
				tc := &message.ToolCall{
					Name: "my_custom_tool",
					Args: map[string]any{"param": "value"},
				}
				tm := &message.Tool{
					Content: message.Content{
						&message.TextBlock{Text: "Tool executed successfully.\nOutput line 2.\nOutput line 3."},
					},
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.GenericToolWidget(chat.ToolExecutionProps{
						ToolCall:    tc,
						ToolMessage: tm,
					}),
				)
			},
		},
		{
			Name: "Error",
			Render: func(c *stage.Context) kitex.Node {
				tc := &message.ToolCall{Name: "my_custom_tool"}
				tm := &message.Tool{
					IsError: true,
					Content: message.Content{
						&message.TextBlock{Text: "something went wrong: exit code 1"},
					},
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.GenericToolWidget(chat.ToolExecutionProps{
						ToolCall:    tc,
						ToolMessage: tm,
					}),
				)
			},
		},
	})

	stg.Register("AgentStatus", []stage.Scene{
		{
			Name: "Processing",
			Render: func(c *stage.Context) kitex.Node {
				phase := c.Select("Phase", []string{"processing", "thinking", "answering"}, "processing")
				tip := c.Text("Active Tip", "")
				seconds := 42

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.AgentStatus(chat.AgentStatusProps{
						Sending:             true,
						ThinkingTime:        seconds,
						LastFinishedTime:    -1,
						RunPromptTokens:     1200,
						RunCompletionTokens: 340,
						IsGenerating:        phase == "answering",
						Phase:               phase,
						ActiveTip:           tip,
					}),
				)
			},
		},
		{
			Name: "Completed",
			Render: func(c *stage.Context) kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.AgentStatus(chat.AgentStatusProps{
						Sending:             false,
						LastFinishedTime:    73,
						RunPromptTokens:     4800,
						RunCompletionTokens: 1200,
					}),
				)
			},
		},
	})

	stg.Register("Bubble", []stage.Scene{
		{
			Name: "User",
			Render: func(c *stage.Context) kitex.Node {
				text := c.Text("Message", "Can you refactor the auth module to use JWT?")
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.Bubble(chat.BubbleProps{
						Role:      message.RoleUser,
						Timestamp: "14:32",
						Children:  []kitex.Node{kitex.Text(text)},
					}),
				)
			},
		},
		{
			Name: "Assistant",
			Render: func(c *stage.Context) kitex.Node {
				text := c.Text("Message", "Sure! I'll refactor the auth module to use JWT tokens.")
				tokensIn := 800
				tokensOut := 240
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.Bubble(chat.BubbleProps{
						Role:         message.RoleAssistant,
						Timestamp:    "14:32",
						TokensInput:  tokensIn,
						TokensOutput: tokensOut,
						Children:     []kitex.Node{kitex.Text(text)},
					}),
				)
			},
		},
		{
			Name: "System Notification",
			Render: func(c *stage.Context) kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.Bubble(chat.BubbleProps{
						Role:                 message.RoleSystem,
						Timestamp:            "14:33",
						IsSystemNotification: true,
						Children:             []kitex.Node{kitex.Text("Session restored.")},
					}),
				)
			},
		},
	})

	stg.Register("BubbleGroup", []stage.Scene{
		{
			Name: "User",
			Render: func(c *stage.Context) kitex.Node {
				text := c.Text("Message", "Can you add dark mode support to the settings panel?")
				msg := message.NewUserText(text)
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.BubbleGroup(chat.BubbleGroupProps{
						Key:  "group-user-1",
						Role: message.RoleUser,
						Msgs: []message.Message{msg},
					}),
				)
			},
		},
		{
			Name: "Assistant",
			Render: func(c *stage.Context) kitex.Node {
				text := c.Text("Message", "I'll add dark mode support. Let me start by updating the theme configuration.")
				msg := message.NewAssistantText(text)
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
				},
					chat.BubbleGroup(chat.BubbleGroupProps{
						Key:  "group-assistant-1",
						Role: message.RoleAssistant,
						Msgs: []message.Message{msg},
					}),
				)
			},
		},
	})

	// ── Tool Widgets ────────────────────────────────────────────────────────

	toolScenes := func(
		name string,
		runningArgs map[string]any,
		successOutput string,
		errorOutput string,
	) []stage.Scene {
		tc := func(args map[string]any) *message.ToolCall {
			return &message.ToolCall{Name: name, Args: args}
		}
		toolMsg := func(out string, isErr bool) *message.Tool {
			return &message.Tool{
				IsError: isErr,
				Content: message.Content{&message.TextBlock{Text: out}},
			}
		}
		wrap := func(node kitex.Node) kitex.Node {
			return kitex.Box(kitex.BoxProps{
				Style: style.S().Padding(2).Width(style.Percent(100)).Height(style.Percent(100)),
			}, node)
		}
		return []stage.Scene{
			{
				Name: "Running",
				Render: func(c *stage.Context) kitex.Node {
					return wrap(chat.ToolExecution(chat.ToolExecutionProps{
						ToolCall: tc(runningArgs),
					}))
				},
			},
			{
				Name: "Success",
				Render: func(c *stage.Context) kitex.Node {
					return wrap(chat.ToolExecution(chat.ToolExecutionProps{
						ToolCall:    tc(runningArgs),
						ToolMessage: toolMsg(successOutput, false),
					}))
				},
			},
			{
				Name: "Error",
				Render: func(c *stage.Context) kitex.Node {
					return wrap(chat.ToolExecution(chat.ToolExecutionProps{
						ToolCall:    tc(runningArgs),
						ToolMessage: toolMsg(errorOutput, true),
					}))
				},
			},
		}
	}

	stg.Register("BashToolWidget", toolScenes(
		"bash",
		map[string]any{"command": "go test ./...", "description": "Run all tests"},
		"ok\n--- PASS: TestFoo (0.01s)\nPASS",
		"--- FAIL: TestBar (0.03s)\n    expected 1, got 2\nFAIL",
	))

	stg.Register("ViewToolWidget", toolScenes(
		"view",
		map[string]any{"path": "internal/tui/views/chat/view.go"},
		"1. package chat\n2. \n3. import (\n4. \t\"context\"\n...",
		"open internal/tui/views/chat/view.go: no such file or directory",
	))

	stg.Register("LsToolWidget", toolScenes(
		"ls",
		map[string]any{"path": "internal/tui/views/chat"},
		"agent_status.go\nbubble.go\ncomposer.go\nview.go",
		"open internal/tui/views/chat: no such file or directory",
	))

	stg.Register("GlobToolWidget", toolScenes(
		"glob",
		map[string]any{"pattern": "**/*.go", "path": "internal/tui"},
		"internal/tui/views/chat/view.go\ninternal/tui/views/chat/bubble.go\ninternal/tui/main.go",
		"no matches found for pattern **/*.go",
	))

	stg.Register("GrepToolWidget", toolScenes(
		"grep",
		map[string]any{"pattern": "kitex.FC", "path": "internal/tui"},
		"internal/tui/views/chat/bubble.go:58:var BubbleGroup = kitex.FC(\"BubbleGroup\",...\ninternal/tui/views/chat/view.go:42:var View = kitex.FC(\"ChatView\",...",
		"error reading directory: permission denied",
	))

	stg.Register("WriteToolWidget", toolScenes(
		"write",
		map[string]any{"path": "internal/tui/views/chat/new_file.go", "content": "package chat\n"},
		"File written successfully.",
		"open internal/tui/views/chat/new_file.go: permission denied",
	))

	stg.Register("EditToolWidget", toolScenes(
		"edit",
		map[string]any{"path": "internal/tui/views/chat/view.go"},
		"Edit applied successfully.",
		"edit failed: old_str not found in file",
	))

	stg.Register("MultiEditToolWidget", toolScenes(
		"multi_edit",
		map[string]any{"path": "internal/tui/views/chat/view.go"},
		"3 edits applied successfully.",
		"multi_edit failed: conflict on edit 2",
	))

	stg.Register("RemoveToolWidget", toolScenes(
		"remove",
		map[string]any{"path": "internal/tui/views/chat/old_file.go"},
		"File removed successfully.",
		"remove internal/tui/views/chat/old_file.go: no such file or directory",
	))

	stg.Register("WebSearchToolWidget", toolScenes(
		"web_search",
		map[string]any{"query": "golang context cancellation best practices"},
		"1. Effective Go - Context\n2. Go Blog: Context and Cancellation\n3. pkg.go.dev/context",
		"search failed: rate limit exceeded",
	))

	stg.Register("WebFetchToolWidget", toolScenes(
		"web_fetch",
		map[string]any{"url": "https://pkg.go.dev/context"},
		"# context\nPackage context defines the Context type...",
		"fetch https://pkg.go.dev/context: connection refused",
	))

	stg.Register("LspSymbolsToolWidget", toolScenes(
		"lsp_symbols",
		map[string]any{"query": "BubbleGroup"},
		"BubbleGroup - internal/tui/views/chat/bubble.go:58\nBubbleGroupProps - internal/tui/views/chat/bubble.go:20",
		"LSP server not running",
	))

	stg.Register("LspDiagnosticsToolWidget", toolScenes(
		"lsp_diagnostics",
		map[string]any{"path": "internal/tui/views/chat/view.go"},
		"No diagnostics found.",
		"LSP server not running for this file",
	))

	stg.Register("ActivateSkillToolWidget", toolScenes(
		"activate_skill",
		map[string]any{"skill": "golang"},
		"Skill 'golang' activated successfully.",
		"skill 'unknown' not found",
	))

	stg.Register("Pulse", []stage.Scene{
		{
			Name: "Default (3 dots)",
			Render: func(c *stage.Context) kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2),
				},
					components.Pulse(components.PulseProps{}),
				)
			},
		},
		{
			Name: "Blinking (1 dot)",
			Render: func(c *stage.Context) kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2),
				},
					components.Pulse(components.PulseProps{
						Count: 1,
					}),
				)
			},
		},
		{
			Name: "Custom Dot Count",
			Render: func(c *stage.Context) kitex.Node {
				count := c.Int("Dot Count", 5)
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2),
				},
					components.Pulse(components.PulseProps{
						Count: count,
					}),
				)
			},
		},
		{
			Name: "Circle Staggered (Breathe)",
			Render: func(c *stage.Context) kitex.Node {
				count := c.Int("Count", 3)
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2),
				},
					components.Pulse(components.PulseProps{
						Stages:    []string{"○", "⊙", "◎", "◉", "●"},
						Count:     count,
						LoopStyle: components.LoopBreathe,
						Interval:  120 * time.Millisecond,
					}),
				)
			},
		},
		{
			Name: "Circle Staggered (Reset)",
			Render: func(c *stage.Context) kitex.Node {
				count := c.Int("Count", 3)
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Padding(2),
				},
					components.Pulse(components.PulseProps{
						Stages:    []string{"○", "⊙", "◎", "◉", "●"},
						Count:     count,
						LoopStyle: components.LoopReset,
						Interval:  120 * time.Millisecond,
					}),
				)
			},
		},
	})

	stg.Run()
}
