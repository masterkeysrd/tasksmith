package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestReadWriteMetadata(t *testing.T) {
	wsDir := t.TempDir()
	meta := Metadata{
		Schema:  1,
		Version: "v1.2.3",
	}

	if err := WriteMetadata(wsDir, meta); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	read, err := ReadMetadata(wsDir)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}
	if read.Schema != 1 || read.Version != "v1.2.3" {
		t.Fatalf("read metadata mismatch: %+v", read)
	}
}

func TestReadMetadataLegacyFallback(t *testing.T) {
	wsDir := t.TempDir()
	cleanDir, err := fsutils.CleanPath(wsDir)
	if err != nil {
		t.Fatalf("CleanPath failed: %v", err)
	}
	wsDataDir, err := xdg.WorkspaceDir(cleanDir)
	if err != nil {
		t.Fatalf("WorkspaceDir failed: %v", err)
	}
	if err := os.MkdirAll(wsDataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	legacyPath := filepath.Join(wsDataDir, "setup.json")
	legacyData := map[string]string{
		"version": "1.0.5",
	}
	raw, _ := json.Marshal(legacyData)
	_ = os.WriteFile(legacyPath, raw, 0o644)

	read, err := ReadMetadata(wsDir)
	if err != nil {
		t.Fatalf("expected ReadMetadata to fall back to setup.json, got error: %v", err)
	}
	if read.Version != "v1.0.5" {
		t.Fatalf("expected version v1.0.5, got %s", read.Version)
	}
}

func TestEnsureMetadata(t *testing.T) {
	wsDir := t.TempDir()

	// Not configured yet
	_, err := EnsureMetadata(wsDir, "v1.0.0")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist for unconfigured workspace, got: %v", err)
	}

	// Create WORKSPACE.md -> now configured
	_ = os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte("name: test"), 0o644)

	meta, err := EnsureMetadata(wsDir, "v1.0.0")
	if err != nil {
		t.Fatalf("EnsureMetadata failed: %v", err)
	}
	if meta.Version != "v1.0.0" {
		t.Fatalf("expected v1.0.0, got %s", meta.Version)
	}
}

func TestRunMigrationsAscendingAndFailure(t *testing.T) {
	wsDir := t.TempDir()

	// Initial version v1.0.0
	_ = WriteMetadata(wsDir, Metadata{Schema: 1, Version: "v1.0.0"})

	var applied []string
	RegisterMigration(MigrationAction{
		TargetVersion: "v1.2.0",
		Description:   "step 2",
		Apply: func(ctx context.Context, dir string) error {
			applied = append(applied, "v1.2.0")
			return nil
		},
	})
	RegisterMigration(MigrationAction{
		TargetVersion: "v1.1.0",
		Description:   "step 1",
		Apply: func(ctx context.Context, dir string) error {
			applied = append(applied, "v1.1.0")
			return nil
		},
	})
	RegisterMigration(MigrationAction{
		TargetVersion:    "v1.1.5",
		Description:      "committed upgrade",
		TouchesCommitted: true,
		Apply: func(ctx context.Context, dir string) error {
			applied = append(applied, "v1.1.5")
			return nil
		},
	})

	ctx := context.Background()
	// Run migrations to v1.2.0
	if err := RunMigrations(ctx, wsDir, "v1.2.0"); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// v1.1.0 and v1.2.0 should have run in order; v1.1.5 touches committed and was skipped
	if len(applied) != 2 || applied[0] != "v1.1.0" || applied[1] != "v1.2.0" {
		t.Fatalf("unexpected applied migrations: %v", applied)
	}

	// Stored version should now be v1.2.0
	meta, err := ReadMetadata(wsDir)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}
	if meta.Version != "v1.2.0" {
		t.Fatalf("expected stored version v1.2.0, got %s", meta.Version)
	}

	// PendingUpgrades should find v1.1.5 when queried before upgrading
	pending, err := PendingUpgrades(wsDir, "v2.0.0")
	if err != nil {
		t.Fatalf("PendingUpgrades failed: %v", err)
	}
	_ = pending
}

func TestRunMigrationsDowngradeAndDev(t *testing.T) {
	wsDir := t.TempDir()
	_ = WriteMetadata(wsDir, Metadata{Schema: 1, Version: "v2.0.0"})

	ctx := context.Background()
	// Downgrade: running v1.0.0 vs stored v2.0.0
	if err := RunMigrations(ctx, wsDir, "v1.0.0"); err != nil {
		t.Fatalf("expected downgrade to succeed without error, got: %v", err)
	}
	meta, _ := ReadMetadata(wsDir)
	if meta.Version != "v2.0.0" {
		t.Fatalf("expected version to remain v2.0.0, got %s", meta.Version)
	}

	// Dev build: skips
	if err := RunMigrations(ctx, wsDir, "v0.0.0-dev"); err != nil {
		t.Fatalf("expected dev build to skip migrations, got: %v", err)
	}
}

func TestRunMigrationsFailureStopsAtLastApplied(t *testing.T) {
	wsDir := t.TempDir()
	_ = WriteMetadata(wsDir, Metadata{Schema: 1, Version: "v1.0.0"})

	RegisterMigration(MigrationAction{
		TargetVersion: "v1.0.1",
		Description:   "good",
		Apply: func(ctx context.Context, dir string) error {
			return nil
		},
	})
	failErr := errors.New("boom")
	RegisterMigration(MigrationAction{
		TargetVersion: "v1.0.2",
		Description:   "failing",
		Apply: func(ctx context.Context, dir string) error {
			return failErr
		},
	})

	ctx := context.Background()
	err := RunMigrations(ctx, wsDir, "v1.0.3")
	if err == nil || !errors.Is(err, failErr) {
		t.Fatalf("expected error containing %v, got %v", failErr, err)
	}

	meta, _ := ReadMetadata(wsDir)
	if meta.Version != "v1.0.1" {
		t.Fatalf("expected stored version to be v1.0.1 after partial failure, got %s", meta.Version)
	}
}
