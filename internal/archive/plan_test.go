package archive_test

import (
	"testing"

	"github.com/hamsa/arca/internal/archive"
)

func TestPlanCountsPerSourceWithoutWriting(t *testing.T) {
	f := newFixture(t)
	f.write(".ssh/id_ed25519", "12345", 0o600)
	f.write(".ssh/config", "123", 0o644)
	f.write("Downloads/a.bin", "1234567890", 0o644)

	cfg := f.cfg(
		f.group("dotfiles", "dotfiles", ".ssh"),
		f.group("downloads", "Downloads", "Downloads"),
	)

	res, err := archive.Plan(cfg, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Stats.Files != 3 || res.Stats.Bytes != 18 {
		t.Errorf("totals = %+v, want 3 files / 18 bytes", res.Stats)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(res.Sources))
	}
	if got := res.Sources[0]; got.Member != "dotfiles/.ssh" || got.Stats.Files != 2 || got.Stats.Bytes != 8 {
		t.Errorf("first source = %+v", got)
	}
	if got := res.Sources[1]; got.Member != "Downloads/Downloads" || got.Stats.Bytes != 10 {
		t.Errorf("second source = %+v", got)
	}

	// A plan must leave nothing behind.
	entries, err := readDirNames(f.outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("plan created %v", entries)
	}
}

func TestPlanSurfacesWarningsBeforeRunning(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "x", 0o644)
	cfg := f.cfg(f.group("g", "", "d"), f.group("missing", "m", "not-there"))

	res, err := archive.Plan(cfg, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("a missing source should be visible at plan time, before any backup runs")
	}
}
