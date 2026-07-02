package chat

import (
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

// IsFeedbackActive signals if an authorization feedback textarea has captured focus
var IsFeedbackActive bool

// ChatController exposes views/chat actions.
type ChatController struct {
	SendQueued func()
	ClearQueue func()
	ScrollDown func()
	ScrollUp   func()
}

// Controller is the pre-allocated, static ChatController instance.
var Controller = &ChatController{}

// AuthController exposes authorization widget actions.
type AuthController struct {
	MoveDown           func()
	MoveUp             func()
	SelectPrevOption   func()
	SelectNextOption   func()
	Approve            func()
	Deny               func()
	StartFeedback      func()
	ToggleCancelDialog func()
	ShowPreview        func()
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

	keymap.Set([]mode.Mode{mode.Normal}, "s", command.ExecFunc("chat:send-queued"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "c", command.ExecFunc("chat:clear-queue"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "J", command.ExecFunc("app:scroll-down"), keymap.Context("chat"))
	keymap.Set([]mode.Mode{mode.Normal}, "K", command.ExecFunc("app:scroll-up"), keymap.Context("chat"))

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
}
