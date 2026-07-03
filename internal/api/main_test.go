package api

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestMain(m *testing.M) {
	xdg.RunWithTestEnv(m, "tasksmith-api-test")
}
