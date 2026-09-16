package formations

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// Flow was retired (form-2fe). Boards written before then must still load so
// the operator can convert or delete the node.
func TestLegacyFlowFormationLoadsReportsAndConverts(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("legacy"), `schema = 1
id = "brd_legacy"
slug = "legacy"
title = "Legacy"
rev = 5

[[mission]]
id = "mis_start"
title = "Start"
goal = "Go"
beadId = ""

[[formation]]
id = "fmn_pipeline"
type = "flow"
title = "New flow"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_plan"
label = "Plan"
agentId = "codex-planner"
harness = "openai-codex"
[[formation.slot]]
id = "slot_execute"
label = "Execute"
[[formation.slot]]
id = "slot_push"
label = "Push"

[[connection]]
id = "edge_start"
from = "mis_start:out"
to = "fmn_pipeline:port_in"
`)
	board, err := store.ReadBoard("legacy")
	if err != nil {
		t.Fatalf("legacy board does not load: %v", err)
	}
	if len(board.Formations) != 1 || board.Formations[0].Type != "flow" || len(board.Formations[0].Slots) != 3 {
		t.Fatalf("legacy formation read as %+v", board.Formations)
	}
	findings := findBoardFindings(ValidateBoard(board).Errors, FindingInvalidFormationType)
	if len(findings) != 1 || findings[0].NodeID != "fmn_pipeline" || !strings.Contains(findings[0].Message, "formation set-type") || !strings.Contains(findings[0].Message, "delete it") {
		t.Fatalf("validation findings = %+v, want one set-type or delete finding", findings)
	}
	personas := NewPersonaStore(filepath.Join(t.TempDir(), "agents"))
	if err := CheckRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_start"}); !errors.Is(err, ErrRunAdmission) || !strings.Contains(err.Error(), `unsupported type "flow"`) {
		t.Fatalf("run admission = %v, want the unsupported type finding", err)
	}

	if _, err := store.CreateFormation("legacy", FormationCreateRequest{Type: "flow"}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); !errors.Is(err, ErrUnsupportedFormationType) || !strings.Contains(err.Error(), "use solo, peer or orchestrated") {
		t.Fatalf("creating flow = %v, want a rejection listing supported types", err)
	}

	converted, err := store.SetFormationType("legacy", FormationTypeRequest{FormationID: "fmn_pipeline", Type: FormationTypeOrchestrated}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatalf("convert legacy flow: %v", err)
	}
	reloaded := mustReadBoard(t, store, "legacy")
	formation := reloaded.Formations[0]
	if reloaded.ETag != converted.ETag || formation.Type != FormationTypeOrchestrated || len(formation.Slots) != 3 || !formation.Slots[0].Controller || formation.Slots[0].AgentID != "codex-planner" || len(reloaded.Connections) != 1 {
		t.Fatalf("converted formation = %+v connections %+v", formation, reloaded.Connections)
	}
	if findings := findBoardFindings(ValidateBoard(reloaded).Errors, FindingInvalidFormationType); len(findings) != 0 {
		t.Fatalf("finding survived conversion: %+v", findings)
	}
}
