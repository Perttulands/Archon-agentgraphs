package formations

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftAuthoringSavesBlankAndPartialFields(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("sketch"), minimalBoard("sketch", 1))
	current := func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("sketch")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}

	mission, err := store.CreateMission("sketch", MissionCreateRequest{}, current())
	if err != nil {
		t.Fatalf("mission without Bead ID or goal: %v", err)
	}
	blankGate, err := store.CreateGate("sketch", GateCreateRequest{}, current())
	if err != nil {
		t.Fatalf("gate with no fields: %v", err)
	}
	partialGate, err := store.CreateGate("sketch", GateCreateRequest{Kinds: []string{"code"}, Check: "output_absent", CheckVersion: "1"}, current())
	if err != nil {
		t.Fatalf("gate with a profile and no check value: %v", err)
	}
	formation, err := store.CreateFormation("sketch", FormationCreateRequest{}, current())
	if err != nil {
		t.Fatalf("formation with no type or title: %v", err)
	}
	if _, err := store.UpdateGate("sketch", GateUpdateRequest{GateID: blankGate.Gate.ID, Check: stringPtr("output_contains")}, current()); err != nil {
		t.Fatalf("gate update naming a profile without version or value: %v", err)
	}

	reloaded, err := store.ReadBoard("sketch")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, ok := findMission(reloaded, mission.Mission.ID); !ok || got.BeadID != "" || got.Title != "Mission" {
		t.Fatalf("reloaded mission = %+v, want draft with default title and no Bead", got)
	}
	if got, ok := findGate(reloaded.Gates, partialGate.Gate.ID); !ok || got.Check != "output_absent" || got.CheckVersion != "1" || got.CheckValue != "" {
		t.Fatalf("reloaded partial gate = %+v, want profile kept with blank value", got)
	}
	if got, ok := findGate(reloaded.Gates, blankGate.Gate.ID); !ok || got.Check != "output_contains" || got.CheckVersion != "" {
		t.Fatalf("reloaded updated gate = %+v, want partial check id", got)
	}
	if got, ok := findFormation(reloaded.Formations, formation.Formation.ID); !ok || got.Type != FormationTypeSolo || len(got.Slots) != 1 {
		t.Fatalf("reloaded formation = %+v, want default solo formation", got)
	}

	malformed := []struct {
		name  string
		write func() error
	}{
		{"unsafe Bead ID", func() error {
			_, err := store.CreateMission("sketch", MissionCreateRequest{BeadID: "Home-123"}, current())
			return err
		}},
		{"unknown profile tuple", func() error {
			_, err := store.CreateGate("sketch", GateCreateRequest{Check: "no_such_profile", CheckVersion: "1"}, current())
			return err
		}},
		{"unknown profile version", func() error {
			_, err := store.CreateGate("sketch", GateCreateRequest{Check: "output_contains", CheckVersion: "999"}, current())
			return err
		}},
		{"unknown profile id without version", func() error {
			_, err := store.CreateGate("sketch", GateCreateRequest{Check: "no_such_profile"}, current())
			return err
		}},
		{"unknown formation type", func() error {
			_, err := store.CreateFormation("sketch", FormationCreateRequest{Type: "bogus"}, current())
			return err
		}},
	}
	for _, test := range malformed {
		if err := test.write(); err == nil {
			t.Errorf("%s saved, want malformed input rejected", test.name)
		}
	}
}

func TestPersonaAuthoringDefaultsBlankKind(t *testing.T) {
	personas := NewPersonaStore(filepath.Join(t.TempDir(), "agents"))
	card, err := personas.CreatePersona(CreatePersonaRequest{ID: "sketch-agent"})
	if err != nil {
		t.Fatalf("persona without kind: %v", err)
	}
	if card.Kind != DefaultPersonaKind {
		t.Fatalf("created kind = %q, want %q", card.Kind, DefaultPersonaKind)
	}
	blank := "  "
	edited, err := personas.EditPersona("sketch-agent", EditPersonaRequest{SetKind: &blank, ExpectedETag: card.ETag})
	if err != nil {
		t.Fatalf("clear persona kind: %v", err)
	}
	if reread, err := personas.ReadPersona("sketch-agent"); err != nil || edited.Kind != DefaultPersonaKind || reread.Kind != DefaultPersonaKind {
		t.Fatalf("cleared kind = %q, reread %+v (%v), want default", edited.Kind, reread, err)
	}
}

const admissionDraftBoard = `schema = 1
id = "brd_draft"
slug = "draft"
title = "Draft"
rev = 3

[[mission]]
id = "mis_main"
title = "Main"
goal = "Ship it"

[[mission]]
id = "mis_idle"
title = "Idle"
goal = ""

[[formation]]
id = "fmn_plan"
type = "solo"
title = "Plan"
[[formation.input]]
id = "port_plan_in"
label = "Input"
[[formation.output]]
id = "port_plan_out"
label = "Output"
[[formation.slot]]
id = "slot_plan"
label = "Planner"

[[formation]]
id = "fmn_build"
type = "orchestrated"
title = "Build"
[[formation.input]]
id = "port_build_in"
label = "Input"
[[formation.output]]
id = "port_build_out"
label = "Output"
[[formation.slot]]
id = "slot_lead"
label = "Lead"
agentId = "codex-builder"
harness = "openai-codex"
[[formation.slot]]
id = "slot_worker"
label = "Worker"
agentId = "nobody-here"

[[formation]]
id = "fmn_sketch"
type = "flow"
title = "Sketch"
[[formation.input]]
id = "port_sketch_in"
label = "Input"
[[formation.output]]
id = "port_sketch_out"
label = "Output"
[[formation.slot]]
id = "slot_sketch"
label = "Step"

[[gate]]
id = "gate_lint"
title = "Lint"
kinds = ["code"]
criterion = ""
check = "output_absent"
checkVersion = "1"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["formation"]
criterion = ""

[[gate]]
id = "gate_unwired"
title = "Unwired"
kinds = ["code"]
criterion = ""

[[connection]]
id = "edge_start"
from = "mis_main:out"
to = "fmn_plan:port_plan_in"

[[connection]]
id = "edge_lint"
from = "fmn_plan:port_plan_out"
to = "gate_lint:in"

[[connection]]
id = "edge_build"
from = "gate_lint:pass"
to = "fmn_build:port_build_in"

[[connection]]
id = "edge_review"
from = "fmn_build:port_build_out"
to = "gate_review:in"
`

func TestRunAdmissionReportsEveryProblemAtOnce(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("draft"), admissionDraftBoard)
	board, err := store.ReadBoard("draft")
	if err != nil {
		t.Fatalf("read draft board: %v", err)
	}
	personas := NewPersonaStore(filepath.Join(t.TempDir(), "agents"))

	report := ValidateRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_main"})
	want := map[string]string{
		FindingUnstaffedSlot + " fmn_plan":              `slot "Planner" (slot_plan) needs an agent`,
		FindingGateNotRoutable + " gate_lint":           "forbidden text for code check output_absent@1",
		FindingGateNotRoutable + " gate_review":         "a judge chain",
		FindingOrchestratedController + " fmn_build":    "exactly one controller slot; it has 0",
		FindingUnavailablePersona + " fmn_build":        `unknown agent "nobody-here"`,
		FindingUnsupportedFormationType + " fmn_sketch": `type "flow"`,
	}
	got := map[string]string{}
	for _, finding := range report.Errors {
		got[finding.Code+" "+finding.NodeID] = finding.Message
	}
	for key, substring := range want {
		if !strings.Contains(got[key], substring) {
			t.Errorf("finding %s = %q, want message containing %q", key, got[key], substring)
		}
	}
	if len(report.Errors) != len(want) {
		t.Errorf("mission-scoped errors = %+v, want exactly %d findings", report.Errors, len(want))
	}
	for _, finding := range append(report.Errors, report.Warnings...) {
		if finding.NodeID == "gate_unwired" || finding.NodeID == "mis_idle" || (finding.NodeID == "fmn_sketch" && finding.Code == FindingUnstaffedSlot) {
			t.Errorf("mission-scoped report includes unreachable draft %+v", finding)
		}
	}

	err = CheckRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_main"})
	var admission *RunAdmissionError
	if !errors.As(err, &admission) || !errors.Is(err, ErrRunAdmission) || len(admission.Findings) != len(report.Errors) {
		t.Fatalf("CheckRunAdmission error = %v, want RunAdmissionError with every finding", err)
	}

	idle := ValidateRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_idle"})
	if len(findBoardFindings(idle.Errors, FindingMissionNotRunnable)) != 1 {
		t.Fatalf("unwired mission errors = %+v, want mission_not_runnable promoted to an error", idle.Errors)
	}

	whole := ValidateRunAdmission(board, personas, RunAdmissionScope{})
	if !hasBoardFinding(whole.Errors, "gate_unwired", "a code check") ||
		!hasBoardFinding(whole.Errors, "fmn_sketch", `slot "Step" (slot_sketch) needs an agent`) ||
		!hasBoardFinding(whole.Warnings, "mis_idle", "no outgoing connection") {
		t.Fatalf("whole-board report errors=%+v warnings=%+v, want every draft marker", whole.Errors, whole.Warnings)
	}
}

func TestRunAdmissionAcceptsCompleteRunPath(t *testing.T) {
	store := NewStore(t.TempDir())
	raw := admissionDraftBoard
	raw = strings.Replace(raw, "id = \"slot_plan\"\nlabel = \"Planner\"", "id = \"slot_plan\"\nlabel = \"Planner\"\nagentId = \"codex-builder\"\nharness = \"openai-codex\"", 1)
	raw = strings.Replace(raw, "checkVersion = \"1\"", "checkVersion = \"1\"\ncheckValue = \"error\"", 1)
	raw = strings.Replace(raw, "harness = \"openai-codex\"\n[[formation.slot]]\nid = \"slot_worker\"", "harness = \"openai-codex\"\ncontroller = true\n[[formation.slot]]\nid = \"slot_worker\"", 1)
	raw = strings.Replace(raw, `agentId = "nobody-here"`, "agentId = \"codex-builder\"\nharness = \"openai-codex\"", 1)
	raw = strings.Replace(raw, `type = "flow"`, `type = "solo"`, 1)
	raw = strings.Replace(raw, "[[connection]]\nid = \"edge_review\"\nfrom = \"fmn_build:port_build_out\"\nto = \"gate_review:in\"\n", "", 1)
	writeFixture(t, store.BoardPath("draft"), raw)
	board, err := store.ReadBoard("draft")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	personas := NewPersonaStore(filepath.Join(t.TempDir(), "agents"))
	if err := CheckRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_main"}); err != nil {
		t.Fatalf("complete run path rejected: %v", err)
	}
	if err := CheckRunAdmission(board, personas, RunAdmissionScope{FormationID: "fmn_plan"}); err != nil {
		t.Fatalf("complete isolated formation rejected: %v", err)
	}
	if err := CheckRunAdmission(board, personas, RunAdmissionScope{FormationID: "fmn_sketch"}); err == nil {
		t.Fatal("isolated unstaffed formation admitted")
	}
}
