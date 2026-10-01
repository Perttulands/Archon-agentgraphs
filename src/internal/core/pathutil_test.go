package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileExists(t *testing.T) {
	// Create a temp file
	tempFile, err := os.CreateTemp("", "fileexists_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	if !FileExists(tempPath) {
		t.Errorf("FileExists(%q) = false, expected true", tempPath)
	}

	if FileExists("/nonexistent/path/file.txt") {
		t.Error("FileExists for nonexistent file = true, expected false")
	}
}

func TestGetAllowedRoots_NormalizesEnvRoots(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(parent); err != nil {
		t.Fatalf("chdir to temp parent: %v", err)
	}

	defer func() {
		_ = os.Chdir(cwd)
		os.Unsetenv("ARCHON_ROOTS")
		ResetConfigForTesting()
	}()
	os.Setenv("ARCHON_ROOTS", " , workspace/. , "+root+" , ")
	ResetConfigForTesting()

	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	want := []string{filepath.Clean(absRoot)}
	if got := GetAllowedRoots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GetAllowedRoots() = %#v, want %#v", got, want)
	}
}

func TestGetAllowedRoots_RootDominatesOtherRoots(t *testing.T) {
	defer func() {
		os.Unsetenv("ARCHON_ROOTS")
		ResetConfigForTesting()
	}()
	os.Setenv("ARCHON_ROOTS", "/, /projects, /workspace/operator")
	ResetConfigForTesting()

	got := GetAllowedRoots()
	want := []string{string(os.PathSeparator)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetAllowedRoots() = %#v, want %#v", got, want)
	}
}
