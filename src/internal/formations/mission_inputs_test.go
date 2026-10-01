package formations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Missions declare named inputs that runs supply and step briefs reference
// (archon-o7p.3).

func inputsBoardFixture(inputs string) string {
	return strings.Replace(s5HumanGateBoardFixture(), `goal = "Ship a showcase"`, `goal = "Ship a showcase"`+"\n"+inputs, 1)
}

const declaredInputs = `inputs = [
  { name = "topic", kind = "text", required = true, description = "What to explore" },
  { name = "sketch", kind = "file", description = "A raw sketch" },
  { name = "repo", kind = "folder" },
]`

func setWorkBriefs(fixture, work, ship string) string {
	fixture = strings.Replace(fixture, "[[formation.input]]\nid = \"port_work_in\"", "[formation.brief]\ngoal = "+renderString(work)+"\n\n[[formation.input]]\nid = \"port_work_in\"", 1)
	return strings.Replace(fixture, "[[formation.input]]\nid = \"port_ship_in\"", "[formation.brief]\ngoal = "+renderString(ship)+"\n\n[[formation.input]]\nid = \"port_ship_in\"", 1)
}

func TestMissionInputsRoundTripThroughTOMLAndAuthoring(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), inputsBoardFixture(declaredInputs))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	want := []MissionInput{
		{Name: "topic", Kind: "text", Required: true, Description: "What to explore"},
		{Name: "sketch", Kind: "file", Description: "A raw sketch"},
		{Name: "repo", Kind: "folder"},
	}
	if !reflect.DeepEqual(board.Missions[0].Inputs, want) {
		t.Fatalf("decoded inputs = %+v, want %+v", board.Missions[0].Inputs, want)
	}
	if report := ValidateBoard(board); len(report.Errors) != 0 {
		t.Fatalf("declared inputs are findings: %+v", report.Errors)
	}

	next := []MissionInput{{Name: "brief", Description: " What to deliver ", Required: true}, {Name: "spec", Kind: "file"}}
	updated, err := store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", Inputs: &next}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	want = []MissionInput{{Name: "brief", Kind: "text", Required: true, Description: "What to deliver"}, {Name: "spec", Kind: "file"}}
	if !reflect.DeepEqual(updated.Missions[0].Inputs, want) || updated.Missions[0].Goal != "Ship a showcase" {
		t.Fatalf("updated Input card = %+v, want inputs %+v and the goal kept", updated.Missions[0], want)
	}
	if !strings.Contains(updated.TOML, "inputs = [\n  { name = \"brief\", kind = \"text\", required = true, description = \"What to deliver\" },\n  { name = \"spec\", kind = \"file\" },\n]\n") {
		t.Fatalf("inputs not written one per line:\n%s", updated.TOML)
	}
	reread, err := store.ReadBoard("session-search")
	if err != nil || !reflect.DeepEqual(reread.Missions[0].Inputs, want) {
		t.Fatalf("reread inputs = %+v (%v)", reread.Missions[0].Inputs, err)
	}
	// The line reader of a file strict decoding refuses reads them too.
	if got := parseMissionNodes([]byte(updated.TOML))[0].Inputs; !reflect.DeepEqual(got, want) {
		t.Fatalf("line reader inputs = %+v, want %+v", got, want)
	}

	for _, bad := range [][]MissionInput{
		{{Name: "Topic"}},
		{{Name: "topic", Kind: "video"}},
		{{Name: "topic"}, {Name: "topic"}},
		{{Name: ""}},
	} {
		if _, err := store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", Inputs: &bad}, WriteOptions{ExpectedETag: reread.ETag, ExpectedRev: reread.Rev}); !errors.Is(err, ErrInvalidMissionInput) {
			t.Fatalf("inputs %+v: err = %v, want ErrInvalidMissionInput", bad, err)
		}
	}

	none := []MissionInput{}
	cleared, err := store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", Inputs: &none}, WriteOptions{ExpectedETag: reread.ETag, ExpectedRev: reread.Rev})
	if err != nil || cleared.Missions[0].Inputs != nil || strings.Contains(cleared.TOML, "inputs =") {
		t.Fatalf("cleared inputs = %+v (%v):\n%s", cleared.Missions[0].Inputs, err, cleared.TOML)
	}
	if got := MissionRunInputs(cleared); !reflect.DeepEqual(got, []MissionInput{{Name: "brief", Kind: "text", Required: true}}) {
		t.Fatalf("a mission without declared inputs takes %+v, want the implicit required brief", got)
	}
}

func TestDeletingAndRestoringAnInputCardKeepsItsInputs(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), inputsBoardFixture(declaredInputs))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	card := board.Missions[0]
	deleted, err := store.DeleteMission("session-search", MissionDeleteRequest{ID: card.ID}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(deleted.Board.TOML, "name = \"topic\"") {
		t.Fatalf("deleting the Input card left its inputs behind:\n%s", deleted.Board.TOML)
	}
	restored, err := store.RestoreNode("session-search", NodeRestoreRequest{Mission: &card}, WriteOptions{ExpectedETag: deleted.Board.ETag, ExpectedRev: deleted.Board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Board.Missions[0].Inputs, card.Inputs) {
		t.Fatalf("restored inputs = %+v, want %+v", restored.Board.Missions[0].Inputs, card.Inputs)
	}
}

func TestValidationReportsUnknownInputReferencesAndBadDeclarations(t *testing.T) {
	fixture := setWorkBriefs(inputsBoardFixture(declaredInputs), "Explore {topic} from {sketch}; keep {braces} and {\"json\": 1} as written. Escaped {{braces}} and {{topic}} are no references.", "Ship {topic} in {repo}.")
	board, err := parseBoard([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	findings := findBoardFindings(ValidateBoard(board).Errors, FindingUnknownInputReference)
	if len(findings) != 1 || findings[0].NodeID != "fmn_work" || findings[0].Message != "Work's brief references {braces}, but the mission has no input named braces; its inputs are topic, sketch, repo" {
		t.Fatalf("unknown reference findings = %+v", findings)
	}

	// Without declared inputs the implicit brief is the only name.
	implicit, err := parseBoard([]byte(setWorkBriefs(s5HumanGateBoardFixture(), "Answer {brief}.", "Ship {topic}.")))
	if err != nil {
		t.Fatal(err)
	}
	findings = findBoardFindings(ValidateBoard(implicit).Errors, FindingUnknownInputReference)
	if len(findings) != 1 || findings[0].NodeID != "fmn_ship" || !strings.Contains(findings[0].Message, "its inputs are brief") {
		t.Fatalf("implicit brief findings = %+v", findings)
	}

	bad, err := parseBoard([]byte(inputsBoardFixture(`inputs = [{ name = "Topic" }, { name = "repo", kind = "url" }, { name = "repo" }]`)))
	if err != nil {
		t.Fatal(err)
	}
	findings = findBoardFindings(ValidateBoard(bad).Errors, FindingInvalidMissionInput)
	if len(findings) != 3 {
		t.Fatalf("bad declarations = %+v, want three findings", findings)
	}
	// A single step's admission reports them too.
	if report := ValidateRunAdmission(bad, nil, RunAdmissionScope{FormationID: "fmn_work"}); len(findBoardFindings(report.Errors, FindingInvalidMissionInput)) != 3 {
		t.Fatalf("single-step admission = %+v, want the declaration findings", report.Errors)
	}
}

func TestAdmissionChecksTheSuppliedInputs(t *testing.T) {
	board, err := parseBoard([]byte(inputsBoardFixture(declaredInputs)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "sketch.md")
	if err := os.WriteFile(file, []byte("sketch"), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(inputs map[string]string) []BoardFinding {
		t.Helper()
		var findings []BoardFinding
		for _, finding := range ValidateRunAdmission(board, nil, RunAdmissionScope{MissionID: "mis_showcase", Inputs: inputs}).Errors {
			switch finding.Code {
			case FindingMissingInput, FindingUnknownInput, FindingInvalidInputValue:
				findings = append(findings, finding)
			}
		}
		return findings
	}
	if got := check(map[string]string{"topic": "video editing", "sketch": " " + file + " ", "repo": dir}); len(got) != 0 {
		t.Fatalf("valid inputs refused: %+v", got)
	}
	cases := []struct {
		inputs map[string]string
		want   []string
	}{
		{map[string]string{}, []string{"input topic is required: What to explore"}},
		{map[string]string{"topic": " \n"}, []string{"input topic is required: What to explore"}},
		{map[string]string{"topic": "x", "colour": "blue"}, []string{`mission session-search has no input named "colour"; its inputs are topic, sketch, repo`}},
		{map[string]string{"topic": "x", "sketch": "sketch.md"}, []string{`input sketch must be the absolute path of an existing file; "sketch.md" is a relative path`}},
		{map[string]string{"topic": "x", "sketch": dir}, []string{"input sketch must be the absolute path of an existing file; " + dir + " is not a regular file"}},
		{map[string]string{"topic": "x", "repo": file}, []string{"input repo must be the absolute path of an existing directory; " + file + " is not a directory"}},
		{map[string]string{"topic": "x", "repo": filepath.Join(dir, "gone")}, []string{"input repo must be the absolute path of an existing directory; " + filepath.Join(dir, "gone") + " does not exist"}},
	}
	for _, tc := range cases {
		got := check(tc.inputs)
		var messages []string
		for _, finding := range got {
			if finding.NodeID != "mis_showcase" {
				t.Errorf("%v: finding names %q, want the Input card", tc.inputs, finding.NodeID)
			}
			messages = append(messages, finding.Message)
		}
		if !reflect.DeepEqual(messages, tc.want) {
			t.Errorf("%v: findings %q, want %q", tc.inputs, messages, tc.want)
		}
	}

	// A mission that declares none needs a nonempty brief, as before inputs.
	implicit, err := parseBoard([]byte(s5HumanGateBoardFixture()))
	if err != nil {
		t.Fatal(err)
	}
	err = CheckRunAdmission(implicit, nil, RunAdmissionScope{MissionID: "mis_showcase", Inputs: map[string]string{"brief": ""}})
	var admission *RunAdmissionError
	if !errors.As(err, &admission) || len(admission.Findings) != 1 || admission.Findings[0].Message != "input brief is required" {
		t.Fatalf("blank brief admission = %v", err)
	}
	if err := CheckRunAdmission(implicit, nil, RunAdmissionScope{FormationID: "fmn_ship", Inputs: map[string]string{"brief": "ship it"}}); err != nil {
		t.Fatalf("single step with a brief refused: %v", err)
	}
	// Board validation supplies no inputs and so checks none.
	if got := findBoardFindings(ValidateRunAdmission(implicit, nil, RunAdmissionScope{}).Errors, FindingMissingInput); len(got) != 0 {
		t.Fatalf("validation reported run inputs: %+v", got)
	}
}

func TestRunsRecordInputsHandThemToTheFirstStepAndSubstituteBriefs(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), setWorkBriefs(inputsBoardFixture(declaredInputs), "Explore {topic}. Sketch: {sketch}. Keep {\"port\": 1}. Write {{topic}} literally, and {{{topic}}}.", "Ship {topic} into {repo}."))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
		Inputs: map[string]string{"repo": " " + repo + " ", "topic": "video editing\nwith captions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantInputs := []RunInput{{Name: "topic", Kind: "text", Value: "video editing\nwith captions"}, {Name: "repo", Kind: "folder", Value: repo}}
	if !reflect.DeepEqual(status.Inputs, wantInputs) {
		t.Fatalf("projected inputs = %+v, want %+v", status.Inputs, wantInputs)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("calls = %v, want work before the human gate", executor.nodeIDs())
	}
	work := executor.calls[0]
	if work.Brief.Goal != "Explore video editing\nwith captions. Sketch: (not supplied). Keep {\"port\": 1}. Write {topic} literally, and {{topic}}." {
		t.Fatalf("work brief = %q", work.Brief.Goal)
	}
	if len(work.Inputs) != 1 || work.Inputs[0].Text != "topic: video editing\nwith captions\nrepo: "+repo {
		t.Fatalf("work input = %+v, want the Input card's name: value lines", work.Inputs)
	}

	// The step behind the human gate runs after a verdict and resume, from the
	// ledger, and still receives the values.
	executor.calls = nil
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	resumed, err := NewRunEngine(store, personas, executor).ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != RunStatusSucceeded || len(executor.calls) != 1 || executor.calls[0].Brief.Goal != "Ship video editing\nwith captions into "+repo+"." {
		t.Fatalf("resumed %s, calls %+v", resumed.Status, executor.calls)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := events[0].Data["brief"]; ok {
		t.Fatalf("run_started still records a brief field: %v", events[0].Data)
	}
}

func TestTheImplicitBriefReachesTheFirstStepUnchanged(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), setWorkBriefs(s5HumanGateBoardFixture(), "Answer the brief: {brief}", "Ship."))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	brief := "  Build the importer.\n\nKeep it small.  "
	executor := &fakeRunExecutor{}
	status, err := NewRunEngine(store, personas, executor).RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
		Inputs: map[string]string{"brief": brief},
	})
	if err != nil {
		t.Fatal(err)
	}
	if work := executor.calls[0]; work.Inputs[0].Text != brief || work.Brief.Goal != "Answer the brief: "+brief {
		t.Fatalf("work input %q brief %q", work.Inputs[0].Text, work.Brief.Goal)
	}
	if !reflect.DeepEqual(status.Inputs, []RunInput{{Name: "brief", Kind: "text", Value: brief}}) {
		t.Fatalf("projected inputs = %+v", status.Inputs)
	}
	// The engine refuses a start whose inputs fail the checks, behind admission.
	_, err = NewRunEngine(store, personas, executor).RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Inputs: map[string]string{},
	})
	var admission *RunAdmissionError
	if !errors.As(err, &admission) || admission.Findings[0].Code != FindingMissingInput {
		t.Fatalf("start without a brief = %v, want the missing input finding", err)
	}
}

func TestASingleStepTakesTheMissionsInputsAndRunFields(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), setWorkBriefs(inputsBoardFixture(declaredInputs), "Explore {topic}.", "Ship {topic} into {repo}."))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	context := t.TempDir()
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunFormation("session-search", "fmn_ship", FormationRunRequest{
		ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
		Inputs:       map[string]string{"topic": "captions"},
		ContextPaths: []string{context},
		BeadID:       "archon-o7p.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusSucceeded || len(executor.calls) != 1 {
		t.Fatalf("single step %s, calls %v", status.Status, executor.nodeIDs())
	}
	call := executor.calls[0]
	if call.Brief.Goal != "Ship captions into (not supplied)." || call.Inputs[0].Text != "topic: captions" {
		t.Fatalf("single step brief %q input %q", call.Brief.Goal, call.Inputs[0].Text)
	}
	if call.MissionBeadID != "archon-o7p.3" || !reflect.DeepEqual(call.ContextPaths, []string{context}) {
		t.Fatalf("single step run fields: bead %q context %v", call.MissionBeadID, call.ContextPaths)
	}
	// The step's brief is the run's objective, resolved like the brief itself.
	if call.MissionGoal != "Ship captions into (not supplied)." {
		t.Fatalf("single step objective = %q, want its references resolved", call.MissionGoal)
	}
	if info, err := os.Stat(call.Cwd); err != nil || !info.IsDir() || filepath.Base(call.Cwd) != status.RunID {
		t.Fatalf("single step cwd %q (%v), want an automatic workspace", call.Cwd, err)
	}
	if !reflect.DeepEqual(status.Inputs, []RunInput{{Name: "topic", Kind: "text", Value: "captions"}}) || status.BeadID != "archon-o7p.3" {
		t.Fatalf("single step projection = %+v", status)
	}

	_, err = engine.RunFormation("session-search", "fmn_ship", FormationRunRequest{Inputs: map[string]string{"repo": t.TempDir()}})
	var admission *RunAdmissionError
	if !errors.As(err, &admission) || admission.Findings[0].Message != "input topic is required: What to explore" {
		t.Fatalf("single step without topic = %v", err)
	}
}

// {{name}} is the one escape: it renders a literal {name} and is never a
// reference, so a brief can say {name} to an agent (archon-o7p.3).
func TestEscapedBracesAreLiteralAndNeverReferences(t *testing.T) {
	declared := []MissionInput{{Name: "topic", Kind: MissionInputText}, {Name: "notes", Kind: MissionInputText}}
	inputs := []RunInput{{Name: "topic", Kind: MissionInputText, Value: "captions"}}
	for text, want := range map[string]string{
		"{topic}":                      "captions",
		"{{topic}}":                    "{topic}",
		"{{unknown}}":                  "{unknown}",
		"{{topic}} is {topic}":         "{topic} is captions",
		"{{notes}} or {notes}":         "{notes} or (not supplied)",
		"{{{topic}}}":                  "{{topic}}",
		"{{Topic}} and {{ topic }}":    "{{Topic}} and {{ topic }}",
		"plain text, {\"json\": true}": "plain text, {\"json\": true}",
	} {
		if got := SubstituteRunInputs(text, declared, inputs); got != want {
			t.Errorf("SubstituteRunInputs(%q) = %q, want %q", text, got, want)
		}
	}
	board, err := parseBoard([]byte(setWorkBriefs(s5HumanGateBoardFixture(), "Answer {brief}; the template says {{name}} and {{brief}}.", "Ship.")))
	if err != nil {
		t.Fatal(err)
	}
	if findings := findBoardFindings(ValidateBoard(board).Errors, FindingUnknownInputReference); len(findings) != 0 {
		t.Fatalf("escaped braces reported as references: %+v", findings)
	}
}
