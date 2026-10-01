package api

import (
	"os"
	"testing"
)

// Tests know only the Codex models a test's own fixture cache names, never
// the host's ~/.codex/models_cache.json.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "archon-codex-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CODEX_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
