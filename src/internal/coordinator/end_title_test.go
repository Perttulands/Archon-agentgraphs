package coordinator

import (
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A run that fails at a rejected End node names it by title in run wait and
// notifications, as it names steps and gates (archon-o7p.10).
func TestNodeTitleNamesEndNodes(t *testing.T) {
	board := &formations.BoardDocument{Ends: []formations.EndNode{{ID: "end_rejected", Title: "Rejected", Outcome: formations.EndOutcomeRejected}}}
	if got := waitTitle(board, "end_rejected"); got != "Rejected" {
		t.Fatalf("waitTitle = %q, want Rejected", got)
	}
}

// The run projection names the End nodes a finished run's paths reached, so
// the cockpit can light them.
func TestProjectionEventsNameTheEndNodesARunReached(t *testing.T) {
	status := &formations.RunStatusProjection{RunID: "run_x", Status: formations.RunStatusFailed, Final: true}
	p := project(status, []formations.RunEvent{
		{Seq: 1, Type: formations.RunEventStarted},
		{Seq: 2, Type: formations.RunEventFailed, NodeID: "end_rejected", GateID: "gate_review", Data: map[string]any{"code": "path_rejected", "endIds": []any{"end_done", "end_rejected"}}},
	})
	last := p.Events[len(p.Events)-1]
	if len(last.EndIDs) != 2 || last.EndIDs[0] != "end_done" || last.EndIDs[1] != "end_rejected" || last.NodeID != "end_rejected" {
		t.Fatalf("run_failed projection = %+v", last)
	}
}
