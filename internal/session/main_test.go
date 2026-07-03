package session

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestMain(m *testing.M) {
	xdg.RunWithTestEnv(m, "tasksmith-session-test")
}
