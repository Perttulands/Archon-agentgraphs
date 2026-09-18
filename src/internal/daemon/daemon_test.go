package daemon

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSessionAsksNameTheArchonCLIBesideTheDaemon(t *testing.T) {
	bin := t.TempDir()
	if got := cliBeside(filepath.Join(bin, "archond")); got != "" {
		t.Fatalf("a daemon without a CLI beside it names %q", got)
	}
	cli := filepath.Join(bin, "archon")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(filepath.Join(bin, "archond")); got != "" {
		t.Fatalf("a CLI that cannot run is named %q", got)
	}
	if err := os.Chmod(cli, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"archond", "formationsd"} {
		if got := cliBeside(filepath.Join(bin, command)); got != cli {
			t.Fatalf("%s names %q, want %q", command, got, cli)
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

func TestFileRootFlagRequiresAnAbsolutePath(t *testing.T) {
	if err := Run([]string{"--file-root", "relative/docs"}); err == nil || !strings.Contains(err.Error(), "--file-root requires an absolute path") {
		t.Fatalf("relative --file-root = %v, want a rejection", err)
	}
}

func TestRunWorkspaceRootFlagRequiresAnAbsolutePath(t *testing.T) {
	if err := Run([]string{"--executor", "lab", "--state-dir", t.TempDir(), "--run-workspace-root", "relative/work"}); err == nil || !strings.Contains(err.Error(), "--run-workspace-root requires an absolute path") {
		t.Fatalf("relative --run-workspace-root = %v, want a rejection", err)
	}
}
