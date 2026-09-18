package formations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomaticRunWorkspacesAreDistinctAndFrozen(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "default"
		if custom {
			name = "configured"
		}
		t.Run(name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
			root := filepath.Join(store.Workspace, "workspaces")
			if custom {
				root = filepath.Join(t.TempDir(), "mission-work")
				store.RunWorkspaceRoot = root
			}
			for range 2 {
				started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
				if err != nil {
					t.Fatal(err)
				}
				events, err := store.ReadRunEvents(started.RunID)
				if err != nil {
					t.Fatal(err)
				}
				want := filepath.Join(root, started.RunID)
				if events[0].Data["cwd"] != want {
					t.Fatalf("frozen cwd = %v, want %s", events[0].Data["cwd"], want)
				}
				info, err := os.Stat(want)
				if err != nil || !info.IsDir() {
					t.Fatalf("workspace missing: %v", err)
				}
				if err := store.AppendRunEvent(started.RunID, RunEvent{Type: RunEventCanceled}); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(want); err != nil {
					t.Fatalf("admitted workspace not retained: %v", err)
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 2 {
				t.Fatalf("workspaces = %v (%v)", entries, err)
			}
		})
	}
}

func TestRunAdmissionRemovesOnlyItsUnusedAutomaticWorkspace(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	root := filepath.Join(store.Workspace, "workspaces")
	req := RunStartRequest{MissionID: "mis_missing", Personas: personas}
	if _, err := store.StartRun("session-search", req); err == nil {
		t.Fatal("missing mission accepted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid admission allocated workspace root: %v", err)
	}
	req.MissionID = "mis_showcase"
	req.Brief = strings.Repeat("x", runtimeAuthorityMaxEventBytes)
	if _, err := store.StartRun("session-search", req); err == nil {
		t.Fatal("oversized admission accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unused workspace left behind: %v (%v)", entries, err)
	}
	existing := t.TempDir()
	marker := filepath.Join(existing, "keep.txt")
	if err := os.WriteFile(marker, []byte("existing project"), 0600); err != nil {
		t.Fatal(err)
	}
	req.Cwd = existing
	if _, err := store.StartRun("session-search", req); err == nil {
		t.Fatal("oversized explicit admission accepted")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "existing project" {
		t.Fatalf("existing workspace altered: %q %v", got, err)
	}
	req.Brief = "Use existing project"
	started, err := store.StartRun("session-search", req)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadRunEvents(started.RunID)
	if err != nil || events[0].Data["cwd"] != existing {
		t.Fatalf("explicit cwd changed: %v %v", events, err)
	}
}
