package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundleDiscoveryDoesNotDependOnWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", "archond")
	if got := uiBeside(executable); got != "" {
		t.Fatalf("source build unexpectedly serves %q", got)
	}
	ui := filepath.Join(root, "share", "archon", "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "index.html"), []byte("Archon"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"archond", "formationsd"} {
		if got := uiBeside(filepath.Join(root, "bin", command)); got != ui {
			t.Fatalf("%s resolves %q, want %q", command, got, ui)
		}
	}
}

func TestNotifyFlagsRequireAnExecutableAndAPlainCockpitURL(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "notify")
	plain := filepath.Join(dir, "plain")
	for path, mode := range map[string]os.FileMode{executable: 0o700, plain: 0o600} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, valid := range [][2]string{{"", ""}, {executable, ""}, {executable, "http://cockpit.example:8091/"}, {"", "https://cockpit.example"}} {
		if err := validateNotifyFlags(valid[0], valid[1]); err != nil {
			t.Fatalf("%q %q: %v", valid[0], valid[1], err)
		}
	}
	for _, invalid := range [][2]string{{"notify", ""}, {filepath.Join(dir, "missing"), ""}, {plain, ""}, {dir, ""}, {"", "cockpit.example"}, {"", "ftp://cockpit.example"}, {"", "http://user:pass@cockpit.example"}, {"", "http://cockpit.example/?board=x"}} {
		if err := validateNotifyFlags(invalid[0], invalid[1]); err == nil {
			t.Fatalf("%q %q accepted", invalid[0], invalid[1])
		}
	}
}
