package store

import (
	"os"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
)

func TestMain(m *testing.M) {
	os.Exit(testutils.RunWithXDG(m, "tasksmith"))
}
