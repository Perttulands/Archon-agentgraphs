package formations

import (
	"errors"
	"strings"
	"testing"
)

func TestFormationExecutionPolicyRoundTripAndInheritance(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), tmuxPeerBoardFixture())
	before, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int{37, 900, 0} {
		updated, err := store.SetFormationExecutionPolicy("session-search", FormationExecutionPolicyRequest{
			FormationID: "fmn_peer", TimeoutSeconds: seconds, UpdatedBy: "agent:test",
		}, WriteOptions{ExpectedRev: before.Rev, ExpectedETag: before.ETag})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatal(err)
		}
		formation, ok := findFormation(loaded.Formations, "fmn_peer")
		if !ok || len(formation.Slots) != 2 || formation.Brief == nil {
			t.Fatalf("lost formation content: %+v", formation)
		}
		if seconds == 0 {
			if formation.Execution != nil || strings.Contains(loaded.TOML, "[formation.execution]") {
				t.Fatalf("override not cleared: %+v", formation.Execution)
			}
		} else if formation.Execution == nil || formation.Execution.TimeoutSeconds != seconds {
			t.Fatalf("policy = %+v, want %d", formation.Execution, seconds)
		}
		if updated.ETag != loaded.ETag {
			t.Fatal("write/read disagree")
		}
		again, err := store.SetFormationExecutionPolicy("session-search", FormationExecutionPolicyRequest{FormationID: "fmn_peer", TimeoutSeconds: seconds}, WriteOptions{ExpectedRev: loaded.Rev, ExpectedETag: loaded.ETag})
		if err != nil {
			t.Fatal(err)
		}
		if again.Rev != loaded.Rev {
			t.Fatalf("no-op bumped revision: %d -> %d", loaded.Rev, again.Rev)
		}
		before = loaded
	}
	_, err = store.SetFormationExecutionPolicy("session-search", FormationExecutionPolicyRequest{FormationID: "fmn_peer", TimeoutSeconds: -1}, WriteOptions{})
	if !errors.Is(err, ErrInvalidExecutionPolicy) {
		t.Fatalf("negative duration: %v", err)
	}
}

func TestFormationExecutionPolicyValidation(t *testing.T) {
	for _, seconds := range []int{-1, 0, int(^uint(0) >> 1)} {
		board := &BoardDocument{Formations: []FormationNode{{ID: "fmn_work", Type: "solo", Execution: &FormationExecutionPolicy{TimeoutSeconds: seconds}}}}
		found := false
		for _, finding := range ValidateBoard(board).Errors {
			found = found || finding.Code == "invalid_execution_policy"
		}
		if !found {
			t.Fatalf("duration %d accepted", seconds)
		}
	}
}
