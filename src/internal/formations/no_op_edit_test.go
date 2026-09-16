package formations

import (
	"testing"
)

// An edit that changes nothing must not write a new revision: other editors
// holding the current ETag would otherwise lose their next write for nothing.
func TestAuthoringEditsThatChangeNothingKeepRevisionAndETag(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	if _, err := store.CreateBoard(BoardCreateRequest{Slug: "same", Title: "Same"}); err != nil {
		t.Fatalf("create board: %v", err)
	}
	current := func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("same")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	mission, err := store.CreateMission("same", MissionCreateRequest{Title: "Work", Goal: "Do it", BeadID: "form-demo"}, current())
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.CreateFormation("same", FormationCreateRequest{Type: FormationTypeSolo, Title: "Worker"}, current())
	if err != nil {
		t.Fatal(err)
	}
	lead, err := store.CreateFormation("same", FormationCreateRequest{Type: FormationTypeOrchestrated, Title: "Lead"}, current())
	if err != nil {
		t.Fatal(err)
	}
	gate, err := store.CreateGate("same", GateCreateRequest{Title: "Review", Kinds: []string{"code"}, Check: "output_contains", CheckVersion: "1", CheckValue: "OK"}, current())
	if err != nil {
		t.Fatal(err)
	}
	human, err := store.CreateGate("same", GateCreateRequest{Title: "Signoff"}, current())
	if err != nil {
		t.Fatal(err)
	}
	title, goal, bead, criterion := "Renamed", "Do it well", "form-other", "Looks right"
	kinds := []string{"code", "human"}
	check, version, value := "output_contains", "1", "DONE"

	for _, test := range []struct {
		name string
		edit func(WriteOptions) error
	}{
		{"assign the same agent and harness", func(opts WriteOptions) error {
			_, err := store.AssignFormationSlot("same", FormationSlotAssignmentRequest{FormationID: worker.Formation.ID, SlotID: worker.Formation.Slots[0].ID, AgentID: "codex-builder", Harness: "openai-codex"}, opts)
			return err
		}},
		{"rename a formation to its title", func(opts WriteOptions) error {
			_, err := store.UpdateFormation("same", FormationUpdateRequest{FormationID: worker.Formation.ID, Title: &title}, opts)
			return err
		}},
		{"set the same brief", func(opts WriteOptions) error {
			_, err := store.SetFormationBrief("same", FormationBriefRequest{FormationID: worker.Formation.ID, Goal: "Produce the result", BeadID: "form-demo", Files: []string{"src/a.go"}, Links: []string{"https://example.com/spec"}}, opts)
			return err
		}},
		{"set a formation to its type", func(opts WriteOptions) error {
			_, err := store.SetFormationType("same", FormationTypeRequest{FormationID: worker.Formation.ID, Type: FormationTypePeer}, opts)
			return err
		}},
		{"make the controller the controller", func(opts WriteOptions) error {
			_, err := store.SetFormationController("same", FormationControllerRequest{FormationID: lead.Formation.ID, SlotID: lead.Formation.Slots[1].ID}, opts)
			return err
		}},
		{"update a gate with its fields", func(opts WriteOptions) error {
			_, err := store.UpdateGate("same", GateUpdateRequest{GateID: gate.Gate.ID, Title: &title, Kinds: kinds, Criterion: &criterion, Check: &check, CheckVersion: &version, CheckValue: &value}, opts)
			return err
		}},
		{"update a mission with its fields", func(opts WriteOptions) error {
			_, err := store.UpdateMission("same", MissionUpdateRequest{MissionID: mission.Mission.ID, Title: &title, Goal: &goal, BeadID: &bead}, opts)
			return err
		}},
		{"attach the same judge chain", func(opts WriteOptions) error {
			_, err := store.SetGateJudgeChain("same", GateJudgeRequest{GateID: gate.Gate.ID, Chain: []string{lead.Formation.ID}}, opts)
			return err
		}},
		{"rename the board to its title", func(opts WriteOptions) error {
			_, err := store.UpdateBoardMetadata("same", BoardMetadataPatch{Title: &title, UpdatedBy: "human:ui"}, opts)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.edit(current()); err != nil {
				t.Fatalf("first edit: %v", err)
			}
			before, err := store.ReadBoard("same")
			if err != nil {
				t.Fatal(err)
			}
			beforeBytes := readFile(t, store.BoardPath("same"))
			if err := test.edit(WriteOptions{ExpectedETag: before.ETag, ExpectedRev: before.Rev}); err != nil {
				t.Fatalf("repeated edit: %v", err)
			}
			after, err := store.ReadBoard("same")
			if err != nil {
				t.Fatal(err)
			}
			if after.Rev != before.Rev || after.ETag != before.ETag || readFile(t, store.BoardPath("same")) != beforeBytes {
				t.Fatalf("repeated edit wrote rev %d -> %d, etag changed %t", before.Rev, after.Rev, after.ETag != before.ETag)
			}
		})
	}

	t.Run("no-op edits return the current board and still check the ETag", func(t *testing.T) {
		before, err := store.ReadBoard("same")
		if err != nil {
			t.Fatal(err)
		}
		returned, err := store.DetachGateJudge("same", GateJudgeRequest{GateID: human.Gate.ID}, WriteOptions{ExpectedETag: before.ETag, ExpectedRev: before.Rev})
		if err != nil || returned.Rev != before.Rev || returned.ETag != before.ETag || len(returned.Gates) != len(before.Gates) {
			t.Fatalf("detach with no judge returned %+v (%v), want the current board", returned, err)
		}
		if _, err := store.ClearFormationBrief("same", FormationBriefClearRequest{FormationID: lead.Formation.ID}, WriteOptions{ExpectedETag: "stale", ExpectedRev: before.Rev}); err != ErrConflict {
			t.Fatalf("no-op edit with a stale ETag = %v, want ErrConflict", err)
		}
		changed, err := store.ClearFormationBrief("same", FormationBriefClearRequest{FormationID: worker.Formation.ID}, WriteOptions{ExpectedETag: before.ETag, ExpectedRev: before.Rev})
		if err != nil || changed.Rev != before.Rev+1 {
			t.Fatalf("a real change after no-ops = %+v (%v), want rev %d", changed, err, before.Rev+1)
		}
	})
}
