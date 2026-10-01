package core

import (
	"fmt"
	"os"
	"testing"
)

func TestGetTmuxTmpdir(t *testing.T) {
	// Save original env
	origTmpdir := os.Getenv("TMUX_TMPDIR")
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer func() {
		os.Setenv("TMUX_TMPDIR", origTmpdir)
		os.Setenv("XDG_RUNTIME_DIR", origXDG)
	}()

	// Test XDG_RUNTIME_DIR fallback
	os.Unsetenv("TMUX_TMPDIR")
	os.Setenv("XDG_RUNTIME_DIR", "/runtime/test-user")
	if result := GetTmuxTmpdir(); result != "/runtime/test-user/tmux" {
		t.Errorf("GetTmuxTmpdir() with XDG_RUNTIME_DIR = %q, expected /runtime/test-user/tmux", result)
	}

	// Test uid fallback when no XDG_RUNTIME_DIR
	os.Unsetenv("TMUX_TMPDIR")
	os.Unsetenv("XDG_RUNTIME_DIR")
	result := GetTmuxTmpdir()
	expected := fmt.Sprintf("/tmp/tmux-%d", os.Getuid())
	if result != expected {
		t.Errorf("GetTmuxTmpdir() with no env = %q, expected %q", result, expected)
	}

	// Test with custom value
	os.Setenv("TMUX_TMPDIR", "/custom/path")
	if result := GetTmuxTmpdir(); result != "/custom/path" {
		t.Errorf("GetTmuxTmpdir() with custom env = %q, expected /custom/path", result)
	}

	// Test with whitespace
	os.Setenv("TMUX_TMPDIR", "  /trimmed  ")
	if result := GetTmuxTmpdir(); result != "/trimmed" {
		t.Errorf("GetTmuxTmpdir() with whitespace = %q, expected /trimmed", result)
	}

	// Test empty string falls through to XDG/uid
	os.Setenv("TMUX_TMPDIR", "")
	os.Unsetenv("XDG_RUNTIME_DIR")
	result = GetTmuxTmpdir()
	if result != expected {
		t.Errorf("GetTmuxTmpdir() with empty string = %q, expected %q", result, expected)
	}
}

func TestGetTmuxEnv(t *testing.T) {
	// Save original env
	original := os.Getenv("TMUX_TMPDIR")
	defer os.Setenv("TMUX_TMPDIR", original)

	os.Setenv("TMUX_TMPDIR", "/test/path")
	env := GetTmuxEnv()

	found := false
	for _, e := range env {
		if e == "TMUX_TMPDIR=/test/path" {
			found = true
			break
		}
	}

	if !found {
		t.Error("GetTmuxEnv() should include TMUX_TMPDIR=/test/path")
	}
}
