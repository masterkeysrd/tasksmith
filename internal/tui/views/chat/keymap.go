package chat

import (
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

// IsFeedbackActive signals if an authorization feedback textarea has captured focus
var IsFeedbackActive bool

// ChatController exposes views/chat actions.
type ChatController struct {
	SendQueued  func()
	ClearQueue  func()
	ScrollDown  func()
	ScrollUp    func()
	ScrollLeft  func()
	ScrollRight func()
	HistoryPrev func()
	HistoryNext func()
}

// Controller is the pre-allocated, static ChatController instance.
var Controller = &ChatController{}

// AuthController exposes authorization widget actions.
type AuthController struct {
	ActiveToolCallID   string
	MoveDown           func()
	MoveUp             func()
	SelectPrevOption   func()
	SelectNextOption   func()
	Approve            func()
	Deny               func()
	StartFeedback      func()
	ToggleCancelDialog func()
	ShowPreview        func()
	ScrollDown         func()
	ScrollUp           func()
	ScrollLeft         func()
	ScrollRight        func()
}

// AuthCtrl is the pre-allocated, static AuthController instance for the inline widget.
var AuthCtrl = &AuthController{}

// ModalAuthCtrl is the pre-allocated, static AuthController instance for the preview modal.
var ModalAuthCtrl = &AuthController{}

func init() {
	// --- Chat Page Commands & Keymaps ---
	command.Register("chat:send-queued", func(ctx command.CommandContext) error {
		if Controller.SendQueued != nil {
			Controller.SendQueued()
		}
		return nil
	})

	command.Register("chat:history-prev", func(ctx command.CommandContext) error {
		if Controller.HistoryPrev != nil {
			Controller.HistoryPrev()
		}
		return nil
	})

	command.Register("chat:history-next", func(ctx command.CommandContext) error {
		if Controller.HistoryNext != nil {
			Controller.HistoryNext()
		}
		return nil
	})

	command.Register("chat:open-history-picker", func(ctx command.CommandContext) error {
		active.SetModal("historypicker")
		return nil
	})

	command.Register("history", func(ctx command.CommandContext) error {
		active.SetModal("historypicker")
		return nil
	})

	command.Register("app:scroll-down", func(ctx command.CommandContext) error {
		if Controller.ScrollDown != nil {
			Controller.ScrollDown()
		}
		return nil
	})

	command.Register("app:scroll-up", func(ctx command.CommandContext) error {
		if Controller.ScrollUp != nil {
			Controller.ScrollUp()
		}
		return nil
	})

	command.Register("app:scroll-left", func(ctx command.CommandContext) error {
		if Controller.ScrollLeft != nil {
			Controller.ScrollLeft()
		}
		return nil
	})

	command.Register("app:scroll-right", func(ctx command.CommandContext) error {
		if Controller.ScrollRight != nil {
			Controller.ScrollRight()
		}
		return nil
	})

	keymap.Set([]mode.Mode{mode.Normal}, "s", command.ExecFunc("chat:send-queued"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "c", command.ExecFunc("chat:clear-queue"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "J", command.ExecFunc("app:scroll-down"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "K", command.ExecFunc("app:scroll-up"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "H", command.ExecFunc("app:scroll-left"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "L", command.ExecFunc("app:scroll-right"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal, mode.Insert}, "<C-r>", command.ExecFunc("chat:open-history-picker"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Insert}, "<C-p>", command.ExecFunc("chat:history-prev"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Insert}, "<C-n>", command.ExecFunc("chat:history-next"), keymap.Context("chat"))

	// --- Generic App Navigation Key Mappings ---
	keymap.Set([]mode.Mode{mode.Normal}, "j", command.ExecFunc("app:move-down"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "k", command.ExecFunc("app:move-up"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "h", command.ExecFunc("app:select-prev"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "l", command.ExecFunc("app:select-next"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "<Enter>", command.ExecFunc("app:accept"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal, mode.Insert}, "<C-c>", command.ExecFunc("app:toggle-cancel"), keymap.Context("chat"))

	// --- View-Specific Key Mappings ---
	keymap.Set([]mode.Mode{mode.Normal}, "d", command.ExecFunc("auth:deny"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "D", command.ExecFunc("auth:start-feedback"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "p", command.ExecFunc("auth:show-preview"), keymap.Context("chat"))

	// --- Inline Auth Widget Command Implementations (Context "chat") ---
	command.Register("app:move-down", func(ctx command.CommandContext) error {
		if AuthCtrl.MoveDown != nil {
			AuthCtrl.MoveDown()
		}
		return nil
	}, command.Context("chat"))
	command.Register("app:move-up", func(ctx command.CommandContext) error {
		if AuthCtrl.MoveUp != nil {
			AuthCtrl.MoveUp()
		}
		return nil
	}, command.Context("chat"))
	command.Register("app:select-prev", func(ctx command.CommandContext) error {
		if AuthCtrl.SelectPrevOption != nil {
			AuthCtrl.SelectPrevOption()
		}
		return nil
	}, command.Context("chat"))
	command.Register("app:select-next", func(ctx command.CommandContext) error {
		if AuthCtrl.SelectNextOption != nil {
			AuthCtrl.SelectNextOption()
		}
		return nil
	}, command.Context("chat"))
	command.Register("app:accept", func(ctx command.CommandContext) error {
		if AuthCtrl.Approve != nil {
			AuthCtrl.Approve()
		}
		return nil
	}, command.Context("chat"))
	command.Register("app:toggle-cancel", func(ctx command.CommandContext) error {
		if AuthCtrl.ToggleCancelDialog != nil {
			AuthCtrl.ToggleCancelDialog()
		}
		return nil
	}, command.Context("chat"))

	command.Register("auth:deny", func(ctx command.CommandContext) error {
		if AuthCtrl.Deny != nil {
			AuthCtrl.Deny()
		}
		return nil
	}, command.Context("chat"))
	command.Register("auth:start-feedback", func(ctx command.CommandContext) error {
		if AuthCtrl.StartFeedback != nil {
			AuthCtrl.StartFeedback()
		}
		return nil
	}, command.Context("chat"))
	command.Register("auth:show-preview", func(ctx command.CommandContext) error {
		if AuthCtrl.ShowPreview != nil {
			AuthCtrl.ShowPreview()
		}
		return nil
	}, command.Context("chat"))

	// --- Modal Auth Widget Command Implementations (Context "modal:auth") ---
	command.Register("app:move-down", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.MoveDown != nil {
			ModalAuthCtrl.MoveDown()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:move-up", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.MoveUp != nil {
			ModalAuthCtrl.MoveUp()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:select-prev", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.SelectPrevOption != nil {
			ModalAuthCtrl.SelectPrevOption()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:select-next", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.SelectNextOption != nil {
			ModalAuthCtrl.SelectNextOption()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:accept", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.Approve != nil {
			ModalAuthCtrl.Approve()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:toggle-cancel", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.ToggleCancelDialog != nil {
			ModalAuthCtrl.ToggleCancelDialog()
		}
		return nil
	}, command.Context("modal:auth"))

	command.Register("auth:deny", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.Deny != nil {
			ModalAuthCtrl.Deny()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("auth:start-feedback", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.StartFeedback != nil {
			ModalAuthCtrl.StartFeedback()
		}
		return nil
	}, command.Context("modal:auth"))

	command.Register("app:scroll-down", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.ScrollDown != nil {
			ModalAuthCtrl.ScrollDown()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:scroll-up", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.ScrollUp != nil {
			ModalAuthCtrl.ScrollUp()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:scroll-left", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.ScrollLeft != nil {
			ModalAuthCtrl.ScrollLeft()
		}
		return nil
	}, command.Context("modal:auth"))
	command.Register("app:scroll-right", func(ctx command.CommandContext) error {
		if ModalAuthCtrl.ScrollRight != nil {
			ModalAuthCtrl.ScrollRight()
		}
		return nil
	}, command.Context("modal:auth"))

	// --- Modal Auth Key Mappings ---
	keymap.Set([]mode.Mode{mode.Normal}, "j", command.ExecFunc("app:move-down"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "k", command.ExecFunc("app:move-up"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "h", command.ExecFunc("app:select-prev"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "l", command.ExecFunc("app:select-next"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "<Enter>", command.ExecFunc("app:accept"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal, mode.Insert}, "<C-c>", command.ExecFunc("app:toggle-cancel"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "d", command.ExecFunc("auth:deny"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "D", command.ExecFunc("auth:start-feedback"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "J", command.ExecFunc("app:scroll-down"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "K", command.ExecFunc("app:scroll-up"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "H", command.ExecFunc("app:scroll-left"), keymap.Context("modal:auth"))
	keymap.Set([]mode.Mode{mode.Normal}, "L", command.ExecFunc("app:scroll-right"), keymap.Context("modal:auth"))

	// --- Compaction TUI Slash Command ---
	command.Register("compact", func(ctx command.CommandContext) error {
		sessionID := active.GetSessionID()
		if sessionID == "" {
			return fmt.Errorf("no active session to compact")
		}
		if tuiapi.GlobalClient == nil {
			return fmt.Errorf("API client not available")
		}
		active.SetStatusMessage("Triggering compaction...")
		go func() {
			_, err := tuiapi.GlobalClient.ForceCompaction(ctx.Ctx, api.ForceCompactionRequest{
				SessionID: sessionID,
			})
			if err != nil {
				active.SetStatusMessage("Compaction failed: " + err.Error())
			} else {
				active.SetStatusMessage("Compaction triggered successfully.")
				if active.InvalidateSessionMessages != nil {
					active.InvalidateSessionMessages(sessionID)
				}
			}
		}()
		return nil
	})
}
