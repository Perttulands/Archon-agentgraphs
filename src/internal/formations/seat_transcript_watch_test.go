package formations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/filewatch"
)

// Exercise the real filesystem observer through WaitTurn: discovery of other
// conversations must not decide the outcome of the selected native turn.
func TestSeatTranscriptDirectoryChanges(t *testing.T) {
	for _, harness := range []string{"claude-code", "openai-codex"} {
		for _, change := range []string{"remove sibling", "move sibling", "remove selected directory", "move selected directory", "remove selected file", "move root"} {
			t.Run(harness+"/"+change, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "transcripts")
				active, sibling := filepath.Join(root, "active"), filepath.Join(root, "sibling", "nested")
				for _, dir := range []string{active, sibling} {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(active, "native.jsonl")
				pointer := seatPointer(filepath.Join(t.TempDir(), "brief.md"))
				if err := os.WriteFile(path, []byte(nativeFixture(harness, root, pointer, "working")), 0600); err != nil {
					t.Fatal(err)
				}
				watch, err := filewatch.New(root)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Close()
				transport := realSeatTransport{command: func(_ context.Context, _ string, _ *strings.Reader, args ...string) (string, error) {
					if args[0] == "display-message" {
						return "0 0 0", nil
					}
					return "esc to interrupt", nil
				}}
				seat := &nativeSeat{variant: HarnessVariant{ID: harness}, root: root, runID: "run_test", watch: watch}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				consumed := 0
				turn, err := transport.WaitTurn(ctx, seat, root, pointer, func(codexTranscriptTurn) error {
					consumed++
					switch change {
					case "remove sibling":
						err = os.RemoveAll(filepath.Dir(sibling))
					case "move sibling":
						err = os.Rename(filepath.Dir(sibling), filepath.Join(t.TempDir(), "moved"))
					case "remove selected directory":
						return os.RemoveAll(active)
					case "move selected directory":
						return os.Rename(active, filepath.Join(t.TempDir(), "moved"))
					case "remove selected file":
						return os.Remove(path)
					case "move root":
						return os.Rename(root, filepath.Join(t.TempDir(), "moved"))
					}
					if err != nil {
						return err
					}
					fresh := filepath.Join(root, "new-conversation")
					if err = os.MkdirAll(fresh, 0700); err != nil {
						return err
					}
					if err = os.WriteFile(filepath.Join(fresh, "other.jsonl"), []byte(nativeFixture(harness, root, "other pointer", "other answer")), 0600); err != nil {
						return err
					}
					final := "answer\n<<<ARCHON-DONE run-id=run_test status=ok artifact=result>>>"
					return os.WriteFile(path, []byte(nativeFixture(harness, root, pointer, final)), 0600)
				})
				if consumed != 1 {
					t.Fatalf("pointer consumed %d times", consumed)
				}
				if strings.HasSuffix(change, "sibling") {
					if err != nil || !turn.Complete {
						t.Fatalf("active turn did not finish after %s: %+v, %v", change, turn, err)
					}
				} else {
					diagnostic := "selected native transcript moved or removed"
					if change == "move root" {
						diagnostic = "transcript root moved or removed"
					}
					if err == nil || !strings.Contains(err.Error(), diagnostic) {
						t.Fatalf("%s: want %q diagnostic, got %v", change, diagnostic, err)
					}
				}
			})
		}
	}
}
