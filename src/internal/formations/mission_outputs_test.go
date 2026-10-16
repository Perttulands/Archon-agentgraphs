package formations

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Per-attempt directories, typed output ports and mission outputs
// (archon-5tg1).

// draftLoopBoardFixture is a step whose file output a code gate checks for
// LINT OK; a fail sends the work back, a pass ends the run done. The mission
// promises the step's file as its report output.
func draftLoopBoardFixture() string {
	return `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedBy = "agent:archon"
updatedAt = "2026-06-03T16:00:00Z"

[[inputCard]]
id = "mis_showcase"
title = "Showcase"
goal = "Ship a showcase"
outputs = [{ name = "report", kind = "file", from = "fmn_work:port_work_out", required = true, description = "The draft that passed lint" }]

[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Draft"
kind = "file"

[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[gate]]
id = "gate_lint"
title = "Lint"
kinds = ["code"]
criterion = "The draft passes lint"
check = "output_contains"
checkVersion = "1"
checkValue = "LINT OK"

[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_gate"
from = "fmn_work:port_work_out"
to = "gate_lint:in"

[[connection]]
id = "edge_gate_fail_work"
from = "gate_lint:fail"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_gate_pass_done"
from = "gate_lint:pass"
to = "end_done:in"
` + branchingBoardEnds()
}

// rewritingExecutor rewrites one workspace file in place on every attempt and
// names it as the step's file output; the third draft passes lint. It also
// leaves notes in the attempt's own directory.
type rewritingExecutor struct {
	path  string
	calls []FormationExecution
}

func (e *rewritingExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	e.calls = append(e.calls, req)
	text := fmt.Sprintf("draft %d", req.Attempt)
	if req.Attempt == 3 {
		text += " LINT OK"
	}
	if err := os.WriteFile(e.path, []byte(text), 0o600); err != nil {
		return FormationExecutionResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.ArtifactDir, "notes.md"), []byte("# notes "+text), 0o600); err != nil {
		return FormationExecutionResult{}, err
	}
	return FormationExecutionResult{Status: "done", Text: text, Outputs: map[string]FormationOutputPayload{
		"port_work_out": {Text: text, Ref: e.path},
	}}, nil
}

func startDraftRun(t *testing.T, fixture string, executor FormationExecutor) (*Store, *RunStatusProjection) {
	t.Helper()
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), fixture)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	if report := ValidateBoard(board); len(report.Errors) > 0 {
		t.Fatalf("fixture findings: %+v", report.Errors)
	}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(NewCodeGateEvaluator())
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	return store, status
}

// A pushback loop that rewrites the same workspace file in place keeps every
// attempt's draft: each accepted output is a snapshot in its own attempt
// directory, readable through the artifact routes, with the verdict that
// judged it.
func TestPushbackLoopKeepsEachAttemptsFileOutput(t *testing.T) {
	draft := filepath.Join(t.TempDir(), "draft.md")
	executor := &rewritingExecutor{path: draft}
	store, status := startDraftRun(t, draftLoopBoardFixture(), executor)
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded on the third draft", status)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("attempts = %d, want 3", len(executor.calls))
	}
	artifacts := filepath.Join(store.Workspace, ".archon", "artifacts", status.RunID)
	for i, call := range executor.calls {
		if want := filepath.Join(artifacts, "fmn_work", fmt.Sprint(i+1)); call.ArtifactDir != want {
			t.Fatalf("attempt %d artifact dir = %q, want %q", i+1, call.ArtifactDir, want)
		}
	}

	evidence, err := store.ProjectRunAttempts(status.RunID, "")
	if err != nil {
		t.Fatalf("attempts: %v", err)
	}
	if len(evidence.Steps) != 1 || evidence.Steps[0].NodeID != "fmn_work" || len(evidence.Steps[0].Attempts) != 3 {
		t.Fatalf("attempts evidence = %+v", evidence)
	}
	if ports := evidence.Steps[0].Outputs; len(ports) != 1 || ports[0].Kind != PortKindFile {
		t.Fatalf("step output ports = %+v", ports)
	}
	paths := map[string]bool{}
	for i, attempt := range evidence.Steps[0].Attempts {
		n := i + 1
		text := fmt.Sprintf("draft %d", n)
		verdict := "fail"
		if n == 3 {
			text, verdict = text+" LINT OK", "pass"
		}
		name := fmt.Sprintf("fmn_work/%d/outputs/port_work_out/draft.md", n)
		if attempt.Attempt != n || attempt.Status != AttemptDone || attempt.Directory == nil || attempt.Directory.Name != fmt.Sprintf("fmn_work/%d", n) {
			t.Fatalf("attempt %d = %+v", n, attempt)
		}
		if len(attempt.Outputs) != 1 {
			t.Fatalf("attempt %d outputs = %+v", n, attempt.Outputs)
		}
		output := attempt.Outputs[0]
		if output.Kind != PortKindFile || output.Artifact != name || output.Path != filepath.Join(artifacts, filepath.FromSlash(name)) || output.FileKind != "markdown" || output.Size != int64(len(text)) {
			t.Fatalf("attempt %d output = %+v", n, output)
		}
		paths[output.Path] = true
		content, err := store.ReadRunArtifact(status.RunID, output.Artifact)
		if err != nil || string(content.Body) != text {
			t.Fatalf("attempt %d snapshot = %q, %v; want %q", n, contentBody(content), err, text)
		}
		if len(attempt.Verdicts) != 1 || attempt.Verdicts[0].GateID != "gate_lint" || attempt.Verdicts[0].Verdict != verdict || attempt.Verdicts[0].Pending {
			t.Fatalf("attempt %d verdicts = %+v", n, attempt.Verdicts)
		}
		var files []string
		for _, file := range attempt.Files {
			files = append(files, file.Name+":"+file.Kind)
		}
		slices.Sort(files)
		if want := []string{fmt.Sprintf("fmn_work/%d/notes.md:markdown", n), name + ":markdown"}; !reflect.DeepEqual(files, want) {
			t.Fatalf("attempt %d files = %v, want %v", n, files, want)
		}
	}
	if len(paths) != 3 {
		t.Fatalf("snapshot paths = %v, want three distinct", paths)
	}
	// The workspace file holds only the last draft; the run promised the
	// snapshot that passed, and run status lists it.
	want := []RunOutput{{Name: "report", Kind: PortKindFile, Value: filepath.Join(artifacts, "fmn_work", "3", "outputs", "port_work_out", "draft.md"), NodeID: "fmn_work", PortID: "port_work_out", Attempt: 3}}
	if !reflect.DeepEqual(status.Outputs, want) {
		t.Fatalf("run outputs = %+v, want %+v", status.Outputs, want)
	}
	projected, err := store.ProjectRun(status.RunID)
	if err != nil || !reflect.DeepEqual(projected.Outputs, want) {
		t.Fatalf("projected outputs = %+v, %v", projected, err)
	}
	only, err := store.ProjectRunAttempts(status.RunID, "fmn_work")
	if err != nil || len(only.Steps) != 1 {
		t.Fatalf("one step's attempts = %+v, %v", only, err)
	}
	if _, err := store.ProjectRunAttempts(status.RunID, "fmn_missing"); err == nil {
		t.Fatal("an unknown step's attempts were served")
	}
}

func contentBody(content *RunArtifactContent) string {
	if content == nil {
		return ""
	}
	return string(content.Body)
}

// siteBoardFixture builds a site folder that a code gate checks. A pass ships
// it through After to the done End; a fail sends it to Fix, which builds its
// own folder and ends done. The Input card declares the given outputs.
func siteBoardFixture(outputs string) string {
	return `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedBy = "agent:archon"
updatedAt = "2026-06-03T16:00:00Z"

[[inputCard]]
id = "mis_showcase"
title = "Showcase"
goal = "Ship a showcase"
outputs = ` + outputs + `
` + siteFormation("work", "folder") + siteFormation("after", "text") + siteFormation("fix", "folder") + `
[[gate]]
id = "gate_lint"
title = "Lint"
kinds = ["code"]
criterion = "The site passes lint"
check = "output_contains"
checkVersion = "1"
checkValue = "LINT OK"
` + branchingBoardConnection("edge_mission_work", "mis_showcase:out", "fmn_work:port_work_in") +
		branchingBoardConnection("edge_work_gate", "fmn_work:port_work_out", "gate_lint:in") +
		branchingBoardConnection("edge_gate_pass", "gate_lint:pass", "fmn_after:port_after_in") +
		branchingBoardConnection("edge_gate_fail", "gate_lint:fail", "fmn_fix:port_fix_in") +
		branchingBoardConnection("edge_after_done", "fmn_after:port_after_out", "end_done:in") +
		branchingBoardConnection("edge_fix_done", "fmn_fix:port_fix_out", "end_done:in") +
		branchingBoardEnds()
}

func siteFormation(name, kind string) string {
	return fmt.Sprintf(`
[[formation]]
id = "fmn_%[1]s"
type = "solo"
title = "%[1]s"

[[formation.input]]
id = "port_%[1]s_in"
label = "Input"

[[formation.output]]
id = "port_%[1]s_out"
label = "Output"
kind = "%[2]s"

[[formation.slot]]
id = "slot_%[1]s"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"
`, name, kind)
}

// siteExecutor builds each folder step's site in its attempt directory. Work's
// text says whether lint passed; After removes the site it was handed when
// removeSite is set, as a careless later step might.
type siteExecutor struct {
	workText   string
	removeSite bool
}

func (e *siteExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	port := req.Formation.Outputs[0]
	if PortKind(port) == PortKindText {
		if e.removeSite {
			if err := os.RemoveAll(req.Inputs[0].Ref); err != nil {
				return FormationExecutionResult{}, err
			}
		}
		return FormationExecutionResult{Status: "done", Text: "shipped", Outputs: map[string]FormationOutputPayload{port.ID: {Text: "shipped"}}}, nil
	}
	site := filepath.Join(req.ArtifactDir, "site")
	if err := os.MkdirAll(site, 0o770); err != nil {
		return FormationExecutionResult{}, err
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("<p>"+e.workText+"</p>"), 0o600); err != nil {
		return FormationExecutionResult{}, err
	}
	return FormationExecutionResult{Status: "done", Text: e.workText, Outputs: map[string]FormationOutputPayload{port.ID: {Text: e.workText, Ref: site}}}, nil
}

func TestMissionOutputsResolveWhereTheRunEnds(t *testing.T) {
	const site = `[{ name = "site", kind = "folder", from = "fmn_work:port_work_out", required = true }]`
	t.Run("a present folder is the run's output", func(t *testing.T) {
		store, status := startDraftRun(t, siteBoardFixture(site), &siteExecutor{workText: "LINT OK"})
		want := []RunOutput{{Name: "site", Kind: PortKindFolder, Value: filepath.Join(store.Workspace, ".archon", "artifacts", status.RunID, "fmn_work", "1", "site"), NodeID: "fmn_work", PortID: "port_work_out", Attempt: 1}}
		if status.Status != RunStatusSucceeded || !reflect.DeepEqual(status.Outputs, want) {
			t.Fatalf("status = %+v, want succeeded with %+v", status, want)
		}
	})
	t.Run("a required folder the run never produced fails it", func(t *testing.T) {
		outputs := `[{ name = "fixed", kind = "folder", from = "fmn_fix:port_fix_out", required = true }]`
		store, status := startDraftRun(t, siteBoardFixture(outputs), &siteExecutor{workText: "LINT OK"})
		failed := lastRunEvent(t, store, status.RunID)
		reason, _ := failed.Data["reason"].(string)
		if status.Status != RunStatusFailed || failed.Type != RunEventFailed || failed.Data["code"] != RunFailureMissingOutput || !strings.Contains(reason, "required output fixed was not produced by fmn_fix:port_fix_out") {
			t.Fatalf("status = %+v, failure = %+v", status, failed)
		}
		if len(status.Outputs) != 0 {
			t.Fatalf("a failed run listed outputs: %+v", status.Outputs)
		}
	})
	t.Run("an optional output the run never produced is left out", func(t *testing.T) {
		outputs := `[{ name = "fixed", kind = "folder", from = "fmn_fix:port_fix_out" }, { name = "site", kind = "folder", from = "fmn_work:port_work_out" }]`
		_, status := startDraftRun(t, siteBoardFixture(outputs), &siteExecutor{workText: "LINT OK"})
		if status.Status != RunStatusSucceeded || len(status.Outputs) != 1 || status.Outputs[0].Name != "site" {
			t.Fatalf("status = %+v, want succeeded with only site", status)
		}
	})
	t.Run("a folder that is gone by the End fails the run", func(t *testing.T) {
		store, status := startDraftRun(t, siteBoardFixture(site), &siteExecutor{workText: "LINT OK", removeSite: true})
		failed := lastRunEvent(t, store, status.RunID)
		reason, _ := failed.Data["reason"].(string)
		if status.Status != RunStatusFailed || failed.Data["code"] != RunFailureMissingOutput || !strings.Contains(reason, "output site must be") || !strings.Contains(reason, "does not exist") {
			t.Fatalf("status = %+v, failure = %+v", status, failed)
		}
	})
	t.Run("a rejected End promises nothing", func(t *testing.T) {
		board := strings.Replace(siteBoardFixture(site), `from = "fmn_fix:port_fix_out"
to = "end_done:in"`, `from = "fmn_fix:port_fix_out"
to = "end_rejected:in"`, 1)
		outputs := `[{ name = "shipped", kind = "text", from = "fmn_after:port_after_out", required = true }]`
		board = strings.Replace(board, site, outputs, 1)
		store, status := startDraftRun(t, board, &siteExecutor{workText: "lint failed"})
		failed := lastRunEvent(t, store, status.RunID)
		if status.Status != RunStatusFailed || failed.Data["code"] != RunFailurePathRejected || len(status.Outputs) != 0 {
			t.Fatalf("status = %+v, failure = %+v; want the path rejection", status, failed)
		}
	})
}

// A file or folder output must name an existing path of its kind; anything
// else blocks the step like a bad ref.
func TestFileOutputThatIsNotThereBlocksTheStep(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ ref, code string }{
		{filepath.Join(dir, "missing.md"), "unavailable_output_ref"},
		{"draft.md", "invalid_output_ref"},
		{"", "invalid_output_ref"},
		{dir, "invalid_output_ref"},
	} {
		store, status := startDraftRun(t, draftLoopBoardFixture(), refExecutor(tc.ref))
		if status.Status != RunStatusBlocked {
			t.Fatalf("ref %q: status = %+v, want blocked", tc.ref, status)
		}
		events, _ := store.ReadRunEvents(status.RunID)
		problem := eventOfType(t, events, RunEventError)
		if problem.Data["code"] != tc.code || problem.NodeID != "fmn_work" {
			t.Fatalf("ref %q: error = %+v, want %s on fmn_work", tc.ref, problem, tc.code)
		}
		for _, event := range events {
			if event.Type == RunEventNodeOutput && event.NodeID == "fmn_work" {
				t.Fatalf("ref %q: a bad file output was accepted: %+v", tc.ref, event)
			}
		}
	}
}

// refExecutor names ref as the step's file output.
type refExecutor string

func (ref refExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	return FormationExecutionResult{Status: "done", Text: "LINT OK", Outputs: map[string]FormationOutputPayload{"port_work_out": {Text: "LINT OK", Ref: string(ref)}}}, nil
}

func lastRunEvent(t *testing.T, store *Store, runID string) RunEvent {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events: %v", err)
	}
	return events[len(events)-1]
}

func TestPortKindAndMissionOutputFindings(t *testing.T) {
	board := strings.Replace(siteBoardFixture(`[
  { name = "site", kind = "file", from = "fmn_work:port_work_out" },
  { name = "loose" },
  { name = "lost", from = "fmn_work:port_gone" },
  { name = "Bad-Name", from = "fmn_work:port_work_out" },
]`), `id = "port_fix_in"
label = "Input"`, `id = "port_fix_in"
label = "Input"
kind = "file"`, 1)
	board = strings.Replace(board, `label = "Output"
kind = "text"`, `label = "Output"
kind = "video"`, 1)
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), board)
	parsed, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	var got []string
	for _, finding := range ValidateBoard(parsed).Errors {
		got = append(got, finding.Code+" "+finding.NodeID+": "+finding.Message)
	}
	want := []string{
		"invalid_port_kind fmn_after: after's output port_after_out has kind \"video\"; use text, file or folder",
		"invalid_port_kind fmn_fix: fix's input port_fix_in has kind \"file\"; only output ports carry a kind",
		"mission_output_kind_mismatch mis_showcase: output site is file, but work's output Output carries folder",
		"unbound_mission_output mis_showcase: output loose is bound to no step output; set its from to <step-id>:<port-id>",
		"unbound_mission_output mis_showcase: output lost is bound to fmn_work:port_gone, which is not a step output port",
		"invalid_mission_output mis_showcase: output name \"Bad-Name\" must start with a lowercase letter and hold only lowercase letters, digits and underscores, such as report or site_dir",
	}
	for _, line := range want {
		if !slices.Contains(got, line) {
			t.Errorf("missing finding %q in:\n%s", line, strings.Join(got, "\n"))
		}
	}

	// A mission run is refused on its outputs; a single step's run promises
	// none, so they do not hold it up.
	codes := func(scope RunAdmissionScope) []string {
		var codes []string
		for _, finding := range ValidateRunAdmission(parsed, nil, scope).Errors {
			if finding.NodeID == "mis_showcase" {
				codes = append(codes, finding.Code)
			}
		}
		return codes
	}
	if got := codes(RunAdmissionScope{MissionID: "mis_showcase"}); !slices.Contains(got, FindingUnboundMissionOutput) || !slices.Contains(got, FindingMissionOutputKindMismatch) {
		t.Errorf("mission admission findings on the Input card = %v, want the output findings", got)
	}
	if got := codes(RunAdmissionScope{FormationID: "fmn_work"}); len(got) != 0 {
		t.Errorf("single-step admission findings on the Input card = %v, want none", got)
	}
}

// Ports and mission outputs round-trip through authoring: a file port added,
// a port's kind changed, outputs declared, and each refused input named.
func TestAuthoringPortKindsAndMissionOutputs(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), siteBoardFixture("[]"))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	opts := func(board *BoardDocument) WriteOptions {
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	if board, err = store.AddFormationPort("session-search", FormationPortRequest{FormationID: "fmn_after", Direction: FormationPortOutput, Label: "Report", Kind: "file", UpdatedBy: "agent:test"}, opts(board)); err != nil {
		t.Fatalf("add file port: %v", err)
	}
	after, _ := findFormation(board.Formations, "fmn_after")
	report := after.Outputs[len(after.Outputs)-1]
	if report.Kind != PortKindFile || report.Label != "Report" {
		t.Fatalf("added port = %+v", report)
	}
	if _, err := store.AddFormationPort("session-search", FormationPortRequest{FormationID: "fmn_after", Direction: FormationPortInput, Label: "In", Kind: "file", UpdatedBy: "agent:test"}, opts(board)); err == nil || !strings.Contains(err.Error(), ErrInvalidPort.Error()) {
		t.Fatalf("a file input port was added: %v", err)
	}
	folder, text := PortKindFolder, PortKindText
	if board, err = store.UpdateFormationPort("session-search", FormationPortUpdateRequest{FormationID: "fmn_after", PortID: report.ID, Kind: &folder, UpdatedBy: "agent:test"}, opts(board)); err != nil {
		t.Fatalf("set port kind: %v", err)
	}
	after, _ = findFormation(board.Formations, "fmn_after")
	if got := after.Outputs[len(after.Outputs)-1]; got.Kind != PortKindFolder || got.Label != "Report" {
		t.Fatalf("updated port = %+v", got)
	}
	if board, err = store.UpdateFormationPort("session-search", FormationPortUpdateRequest{FormationID: "fmn_work", PortID: "port_work_out", Kind: &text, UpdatedBy: "agent:test"}, opts(board)); err != nil {
		t.Fatalf("set port kind text: %v", err)
	}
	if work, _ := findFormation(board.Formations, "fmn_work"); work.Outputs[0].Kind != "" || strings.Contains(board.TOML, `id = "port_work_out"
label = "Output"
kind`) {
		t.Fatalf("a text port kept its kind: %+v\n%s", work.Outputs[0], board.TOML)
	}
	video, blank := "video", " "
	for _, bad := range []FormationPortUpdateRequest{
		{FormationID: "fmn_after", PortID: report.ID, Kind: &video},
		{FormationID: "fmn_after", PortID: "port_after_in", Kind: &folder},
		{FormationID: "fmn_after", PortID: report.ID, Label: &blank},
	} {
		if _, err := store.UpdateFormationPort("session-search", bad, opts(board)); err == nil || !strings.Contains(err.Error(), ErrInvalidPort.Error()) {
			t.Fatalf("port update %+v = %v, want invalid_port", bad, err)
		}
	}

	outputs := []MissionOutput{{Name: "report", Kind: "folder", From: "fmn_after:" + report.ID, Required: true, Description: "The report folder"}, {Name: "notes"}}
	if board, err = store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", Outputs: &outputs, UpdatedBy: "agent:test"}, opts(board)); err != nil {
		t.Fatalf("declare outputs: %v", err)
	}
	want := []MissionOutput{{Name: "report", Kind: "folder", From: "fmn_after:" + report.ID, Required: true, Description: "The report folder"}, {Name: "notes", Kind: "text"}}
	if got := MissionOutputs(board); !reflect.DeepEqual(got, want) {
		t.Fatalf("declared outputs = %+v, want %+v", got, want)
	}
	if reread, err := store.ReadBoard("session-search"); err != nil || !reflect.DeepEqual(MissionOutputs(reread), want) {
		t.Fatalf("outputs did not round-trip: %+v, %v\n%s", reread, err, board.TOML)
	}
	for _, bad := range [][]MissionOutput{
		{{Name: "Report"}},
		{{Name: "report", Kind: "video"}},
		{{Name: "report"}, {Name: "report"}},
		{{Name: "report", From: "nowhere"}},
	} {
		if _, err := store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", Outputs: &bad, UpdatedBy: "agent:test"}, opts(board)); err == nil || !strings.Contains(err.Error(), ErrInvalidMissionOutput.Error()) {
			t.Fatalf("outputs %+v = %v, want invalid_mission_output", bad, err)
		}
	}
	none := []MissionOutput{}
	if board, err = store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", Outputs: &none, UpdatedBy: "agent:test"}, opts(board)); err != nil || len(MissionOutputs(board)) != 0 || strings.Contains(board.TOML, "outputs =") {
		t.Fatalf("clearing outputs = %v\n%s", err, board.TOML)
	}
}

// A seat's prompt names its attempt's own directory and says what each output
// port carries.
func TestSeatPromptNamesTheAttemptDirectoryAndOutputKinds(t *testing.T) {
	formation := FormationNode{ID: "fmn_work", Outputs: []FormationPort{{ID: "port_text"}, {ID: "port_file", Kind: PortKindFile}, {ID: "port_site", Kind: PortKindFolder}}}
	req := FormationExecution{RunID: "run", NodeID: "fmn_work", Formation: formation, Attempt: 2, ArtifactDir: "/state/.archon/artifacts/run/fmn_work/2"}
	prompt := (&TmuxFormationExecutor{config: TmuxExecutorConfig{Cwd: t.TempDir()}}).renderPromptWithContext(req, FormationSlot{ID: "worker"}, PersonaCard{ID: "builder"}, HarnessVariant{ID: "openai-codex"}, "", outputContractExtraLines(formation))
	for _, want := range []string{
		"attempt artifact directory: /state/.archon/artifacts/run/fmn_work/2\n",
		`"port_file": {"text": "one-line summary", "ref": "/absolute/path/to/the-file"},`,
		`"port_site": {"text": "one-line summary", "ref": "/absolute/path/to/the-folder"}`,
		"Text outputs (port_text):",
		"File outputs (port_file): ref is required",
		"Folder outputs (port_site): ref is required",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
}

// A folder snapshot copies files, keeps symlinks as links, leaves out the run's
// own artifact directories inside it, and stays put when the source is
// rewritten in place, so it is a copy and never a hard link.
func TestSnapshotOutputCopiesAFolderOnce(t *testing.T) {
	source := filepath.Join(t.TempDir(), "site")
	artifacts := filepath.Join(source, ".archon", "artifacts", "run_x")
	for _, dir := range []string{filepath.Join(source, "pages"), artifacts} {
		if err := os.MkdirAll(dir, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	page := filepath.Join(source, "pages", "index.html")
	for path, body := range map[string]string{page: "first", filepath.Join(artifacts, "old.md"): "artifact"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("pages/index.html", filepath.Join(source, "home.html")); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "outputs", "port_site")
	copied, err := snapshotOutput(source, parent, []string{artifacts})
	if err != nil || copied != filepath.Join(parent, "site") {
		t.Fatalf("snapshot = %q, %v", copied, err)
	}
	if err := os.WriteFile(page, []byte("rewritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(copied, "pages", "index.html")); err != nil || string(body) != "first" {
		t.Fatalf("snapshot page = %q, %v; want the first version", body, err)
	}
	if link, err := os.Readlink(filepath.Join(copied, "home.html")); err != nil || link != "pages/index.html" {
		t.Fatalf("snapshot link = %q, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(copied, ".archon", "artifacts", "run_x")); !os.IsNotExist(err) {
		t.Fatalf("the run's artifact directory was copied: %v", err)
	}
	// Accepting the same attempt again replaces its earlier copy.
	if again, err := snapshotOutput(source, parent, []string{artifacts}); err != nil || again != copied {
		t.Fatalf("second snapshot = %q, %v", again, err)
	}
	if body, _ := os.ReadFile(filepath.Join(copied, "pages", "index.html")); string(body) != "rewritten" {
		t.Fatalf("second snapshot page = %q", body)
	}
}
