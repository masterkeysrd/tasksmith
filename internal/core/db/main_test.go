package db_test

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestMain(m *testing.M) {
	xdg.RunWithTestEnv(m, "tasksmith-db-test")
}
