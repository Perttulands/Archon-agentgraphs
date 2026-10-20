package formations

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func roleReferenceFixture(t *testing.T) (*Store, *PersonaStore, *PersonaCard, *BoardDocument) {
	t.Helper()
	store, personas := s4RunFixture(t)
	if err := store.withRoleReferencesLock(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	card, err := personas.CreatePersona(CreatePersonaRequest{ID: "writer", Kind: "builder"})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, store.BoardPath("session-search"), strings.ReplaceAll(s4RunBoardFixture(), `agentId = "scout"`, `agentId = ""`))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	return store, personas, card, board
}

func assignReference(store *Store, board *BoardDocument) error {
	_, err := store.AssignFormationSlot(board.Slug, FormationSlotAssignmentRequest{FormationID: "fmn_research", SlotID: "slot_research", AgentID: "writer", Harness: "openai-codex", Effort: "medium"}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	return err
}

// Wait until the public operation owns the actual advisory reference lock,
// while its lower-level mission/persona lock is deliberately held by the test.
func awaitRoleReferenceLock(t *testing.T, store *Store) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(store.Workspace, ".archon", "role-references.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not acquire the reference lock")
}

func TestRoleReferenceWriteBeforeDeleteRejectsDeletion(t *testing.T) {
	for _, writer := range []string{"assign", "restore slots", "restore node"} {
		t.Run(writer, func(t *testing.T) {
			store, personas, card, board := roleReferenceFixture(t)
			// Hold only the mission lock so the public writer acquires the outer
			// reference lock, then waits here before publishing its role reference.
			missionLock := mutexFor(store.BoardPath(board.Slug) + ".lock")
			missionLock.Lock()
			unlocked := false
			defer func() {
				if !unlocked {
					missionLock.Unlock()
				}
			}()
			written := make(chan error, 1)
			go func() {
				offline := NewStore(store.Workspace)
				opts := WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
				switch writer {
				case "assign":
					written <- assignReference(offline, board)
				case "restore slots":
					slots := append([]FormationSlot{}, board.Formations[0].Slots...)
					slots[0].AgentID = "writer"
					_, err := offline.SetFormationType(board.Slug, FormationTypeRequest{FormationID: board.Formations[0].ID, Type: FormationTypeSolo, Slots: slots}, opts)
					written <- err
				case "restore node":
					formation := board.Formations[0]
					formation.ID = "fmn_restored"
					formation.Slots = append([]FormationSlot{}, formation.Slots...)
					formation.Slots[0].ID = "slot_restored"
					formation.Slots[0].AgentID = "writer"
					formation.Inputs = []FormationPort{{ID: "port_restored_in", Label: "Input"}}
					formation.Outputs = []FormationPort{{ID: "port_restored_out", Label: "Output"}}
					_, err := offline.RestoreNode(board.Slug, NodeRestoreRequest{Formation: &formation}, opts)
					written <- err
				}
			}()
			awaitRoleReferenceLock(t, store)
			deleted := make(chan error, 1)
			go func() { _, err := store.DeleteUnusedRole(personas, card.ID, card.ETag); deleted <- err }()
			missionLock.Unlock()
			unlocked = true
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			if err := <-deleted; !errors.Is(err, ErrRoleInUse) {
				t.Fatalf("delete after %s = %v", writer, err)
			}
			if _, err := personas.ReadPersona(card.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRoleDeletionBeforeAssignmentPreservesMissingRoleDraftSemantics(t *testing.T) {
	store, personas, card, board := roleReferenceFixture(t)
	personaLock := mutexFor(filepath.Join(personas.AgentsDir, card.ID+".toml.lock"))
	personaLock.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			personaLock.Unlock()
		}
	}()
	deleted := make(chan error, 1)
	go func() { _, err := store.DeleteUnusedRole(personas, card.ID, card.ETag); deleted <- err }()
	awaitRoleReferenceLock(t, store)
	written := make(chan error, 1)
	go func() { written <- assignReference(NewStore(store.Workspace), board) }()
	personaLock.Unlock()
	unlocked = true
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatalf("missing-role drafts should remain writable: %v", err)
	}
	current, err := store.ReadBoard(board.Slug)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range ValidateRunAdmission(current, personas, RunAdmissionScope{FormationID: "fmn_research"}).Errors {
		found = found || finding.Code == FindingUnavailablePersona && strings.Contains(finding.Message, `"writer"`)
	}
	if !found {
		t.Fatal("admission did not diagnose the later missing-role draft")
	}
}

func TestDeleteUnusedRolePreservesPreconditionsAndBuiltinFallback(t *testing.T) {
	store, personas, card, _ := roleReferenceFixture(t)
	for _, tc := range []struct {
		etag string
		want error
	}{{"", ErrPreconditionRequired}, {"stale", ErrConflict}} {
		if _, err := store.DeleteUnusedRole(personas, card.ID, tc.etag); !errors.Is(err, tc.want) {
			t.Fatalf("etag %q: %v", tc.etag, err)
		}
	}
	builtin, err := personas.ReadPersona("judge")
	if err != nil {
		t.Fatal(err)
	}
	summary := "Custom judge"
	override, err := personas.EditPersona("judge", EditPersonaRequest{SetSummary: &summary, ExpectedETag: builtin.ETag})
	if err != nil {
		t.Fatal(err)
	}
	if remains, err := store.DeleteUnusedRole(personas, "judge", override.ETag); err != nil || !remains {
		t.Fatalf("override delete: %v %v", remains, err)
	}
	if restored, err := personas.ReadPersona("judge"); err != nil || restored.Customized || restored.ETag != builtin.ETag {
		t.Fatalf("builtin fallback: %+v %v", restored, err)
	}
}

// This subprocess uses the same public store writer as the offline CLI. Its
// separate mutex registry proves that the reference lock is process-shared.
func TestRoleReferenceLockAcrossProcesses(t *testing.T) {
	const key = "ARCHON_ROLE_REFERENCE_TEST_WORKSPACE"
	if workspace := os.Getenv(key); workspace != "" {
		store := NewStore(workspace)
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("writer ready")
		if err := assignReference(store, board); err != nil {
			t.Fatal(err)
		}
		return
	}
	store, _, _, _ := roleReferenceFixture(t)
	command := exec.Command(os.Args[0], "-test.run=^TestRoleReferenceLockAcrossProcesses$")
	command.Env = append(os.Environ(), key+"="+store.Workspace)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	done := make(chan error, 1)
	err = store.withRoleReferencesLock(func() error {
		if err := command.Start(); err != nil {
			return err
		}
		go func() { done <- command.Wait() }()
		scanner := bufio.NewScanner(output)
		if !scanner.Scan() || scanner.Text() != "writer ready" {
			return fmt.Errorf("child did not reach assignment: %s", scanner.Text())
		}
		select {
		case err := <-done:
			return fmt.Errorf("offline writer escaped held reference lock: %v", err)
		case <-time.After(30 * time.Millisecond):
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		command.Process.Kill()
		t.Fatal("offline writer did not resume after reference unlock")
	}
	usage, err := store.RoleUsage("writer")
	if err != nil || len(usage.Usage) != 1 {
		t.Fatalf("offline assignment: %+v %v", usage, err)
	}
}
