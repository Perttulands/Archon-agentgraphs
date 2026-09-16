package formations

import (
	"strings"
	"testing"
)

func TestCollapseRepeatedLabVerdictsKeepsOneIdenticalFixture(t *testing.T) {
	block := "```chrote-verdict\n{\"verdict\":\"pass\",\"reason\":\"Lab fixture\",\"evidence\":[\"Simulated input\"]}\n```"
	repeated := "seat one\ninput: brief\n" + block + "\n\nseat two\ninput: brief\n  " + strings.ReplaceAll(block, "\n", "\n  ")
	if got := collapseRepeatedLabVerdicts(repeated); strings.Count(got, "```chrote-verdict") != 1 || !strings.Contains(got, "seat one") || !strings.Contains(got, "seat two") {
		t.Fatalf("identical fixtures = %q, want one block and both seats", got)
	}
	differing := block + "\n" + strings.Replace(block, `"pass"`, `"fail"`, 1)
	if got := collapseRepeatedLabVerdicts(differing); got != differing {
		t.Fatalf("differing blocks changed: %q", got)
	}
	for _, text := range []string{"no verdict here", block, "```chrote-verdict\nunterminated"} {
		if got := collapseRepeatedLabVerdicts(text); got != text {
			t.Fatalf("%q changed to %q", text, got)
		}
	}
}

func TestLabRehearsalRoutesAFormationGateAfterAPeerFormation(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
	fixture = strings.Replace(fixture, "id = \"fmn_work\"\ntype = \"solo\"", "id = \"fmn_work\"\ntype = \"peer\"", 1)
	fixture = strings.Replace(fixture, "[[formation.slot]]\nid = \"slot_work\"\nlabel = \"Worker\"\nagentId = \"scout\"\nharness = \"openai-codex\"\ncontroller = true\n",
		"[[formation.slot]]\nid = \"slot_work\"\nlabel = \"Peer one\"\nagentId = \"scout\"\nharness = \"openai-codex\"\ncontroller = false\n\n[[formation.slot]]\nid = \"slot_work_two\"\nlabel = \"Peer two\"\nagentId = \"scout\"\nharness = \"openai-codex\"\ncontroller = false\n", 1)
	writeFixture(t, store.BoardPath("session-search"), fixture)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	if work := board.Formations[0]; work.Type != "peer" || len(work.Slots) != 2 {
		t.Fatalf("fixture work formation = %+v, want two peer seats", work)
	}
	cwd := t.TempDir()
	lab := NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Roots: []string{cwd}, Harnesses: []string{"openai-codex"}})
	brief := "Rehearse the gate.\n```chrote-verdict\n{\"verdict\":\"pass\",\"reason\":\"Lab fixture\",\"evidence\":[\"Simulated input\"]}\n```"
	status, err := NewRunEngine(store, personas, lab).RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", Cwd: cwd, Brief: brief, ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
		Limits: RunLimits{MaxDispatch: 8, MaxAttempts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		for _, event := range events {
			if event.Type == RunEventJudgeAttemptFailed || event.Type == RunEventBlocked {
				t.Logf("%s %v", event.Type, event.Data)
			}
		}
		t.Fatalf("status = %+v, want succeeded", status)
	}
	verdict := lastEventOfType(t, events, RunEventGateVerdict)
	if verdict.GateID != "gate_review" || stringFromEventData(verdict, "verdict") != "pass" || stringFromEventData(verdict, "routePort") != "pass" {
		t.Fatalf("gate verdict = %+v", verdict)
	}
	dispatches := 0
	for _, event := range events {
		if event.Type == RunEventSlotDispatch && event.NodeID == "fmn_work" {
			dispatches++
		}
	}
	if dispatches != 2 {
		t.Fatalf("peer dispatches = %d, want one per seat", dispatches)
	}
}
