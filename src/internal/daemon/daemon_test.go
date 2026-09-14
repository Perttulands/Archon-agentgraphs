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
