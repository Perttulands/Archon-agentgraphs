package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// archond migrates legacy slots before it serves and says what each now runs.
func TestStartupMigratesLegacySlots(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	path := store.BoardPath("legacy")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	board := "schema = 1\nid = \"brd_legacy\"\nslug = \"legacy\"\ntitle = \"Legacy\"\nrev = 1\n\n[[formation]]\nid = \"fmn_a\"\ntype = \"solo\"\ntitle = \"A\"\n\n[[formation.slot]]\nid = \"slot_a\"\nlabel = \"A\"\nagentId = \"delivery-final-reviewer\"\nharness = \"openai-codex\"\ncontroller = false\n\n[[formation.slot]]\nid = \"slot_b\"\nlabel = \"B\"\nagentId = \"gone\"\nharness = \"claude-code\"\ncontroller = false\n"
	if err := os.WriteFile(path, []byte(board), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writeSlotMigration(&out, store, formations.NewPersonaStore(t.TempDir()))
	text := out.String()
	for _, want := range []string{
		"Archon slot migration: legacy fmn_a/slot_a now runs openai-codex · gpt-6-astra · medium (launch unchanged: true)",
		`Archon slot migration: legacy fmn_a/slot_b left as it was: formations file not found: role "gone"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("startup output missing %q:\n%s", want, text)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `model = "gpt-6-astra"`) {
		t.Fatalf("board after startup migration (%v):\n%s", err, raw)
	}
}
