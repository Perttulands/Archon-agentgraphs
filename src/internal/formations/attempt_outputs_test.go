package formations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Kept outputs (archon-5tg1): each attempt's file and folder outputs are
// read-only copies in its own directory, whatever later steps do with the
// paths they are handed.

// thawAfter gives the owner write access to the read-only copies under root
// when the test ends, so its temporary directory can be removed.
func thawAfter(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		if err := removeKept(root); err != nil {
			t.Errorf("thaw %s: %v", root, err)
		}
	})
}

func readOnly(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("%s is writable: %v", path, info.Mode())
	}
}

func readBody(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// A folder snapshot holds the folder as it was. A link within the folder stays
// a link, pointing at the copy; a link out of it, absolute or relative, to a
// file or a folder, becomes a copy of what it pointed at; a dangling link
// stays as it is, and a link to a folder holding the folder ends. The run's
// artifact directories inside it are left out. The copy is read-only, and
// accepting the attempt again replaces it.
func TestSnapshotOutputKeepsAFolderAsItWas(t *testing.T) {
	work := t.TempDir()
	source := filepath.Join(work, "site")
	artifacts := filepath.Join(source, ".archon", "artifacts", "run_x")
	for _, dir := range []string{filepath.Join(source, "pages"), artifacts, filepath.Join(work, "assets")} {
		if err := os.MkdirAll(dir, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	page := filepath.Join(source, "pages", "index.html")
	shared := filepath.Join(work, "shared.png")
	logo := filepath.Join(work, "assets", "logo.svg")
	write := func(round string) {
		for path, body := range map[string]string{page: "page " + round, shared: "shared " + round, logo: "logo " + round, filepath.Join(artifacts, "old.md"): "artifact"} {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("1")
	for name, target := range map[string]string{
		"home.html":     "pages/index.html",
		"abs-home.html": page,
		"abs.png":       shared,
		"rel.png":       "../shared.png",
		"assets":        "../assets",
		"gone.png":      "missing.png",
		"up":            "..",
	} {
		if err := os.Symlink(target, filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	outputs := t.TempDir()
	thawAfter(t, outputs)
	parent := filepath.Join(outputs, "outputs", "port_site")
	copied, err := snapshotOutput(context.Background(), source, parent, []string{artifacts})
	if err != nil || copied != filepath.Join(parent, "site") {
		t.Fatalf("snapshot = %q, %v", copied, err)
	}
	write("2")
	for path, want := range map[string]string{
		"pages/index.html": "page 1",
		"home.html":        "page 1",
		"abs-home.html":    "page 1",
		"abs.png":          "shared 1",
		"rel.png":          "shared 1",
		"assets/logo.svg":  "logo 1",
	} {
		if got := readBody(t, filepath.Join(copied, path)); got != want {
			t.Errorf("snapshot %s = %q, want %q", path, got, want)
		}
	}
	for name, want := range map[string]string{"home.html": "pages/index.html", "abs-home.html": "pages/index.html", "gone.png": "missing.png"} {
		if link, err := os.Readlink(filepath.Join(copied, name)); err != nil || link != want {
			t.Errorf("snapshot link %s = %q, %v; want %q", name, link, err, want)
		}
	}
	for _, name := range []string{"abs.png", "rel.png", "assets", "up"} {
		if info, err := os.Lstat(filepath.Join(copied, name)); err != nil || info.Mode()&fs.ModeSymlink != 0 {
			t.Errorf("snapshot %s is %v, %v; want a copy", name, info, err)
		}
	}
	if _, err := os.Stat(filepath.Join(copied, ".archon", "artifacts", "run_x")); !os.IsNotExist(err) {
		t.Fatalf("the run's artifact directory was copied: %v", err)
	}
	if err := filepath.WalkDir(copied, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type()&fs.ModeSymlink == 0 {
			readOnly(t, path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Accepting the same attempt again replaces its earlier copy.
	if again, err := snapshotOutput(context.Background(), source, parent, []string{artifacts}); err != nil || again != copied {
		t.Fatalf("second snapshot = %q, %v", again, err)
	}
	if got := readBody(t, filepath.Join(copied, "pages", "index.html")); got != "page 2" {
		t.Fatalf("second snapshot page = %q", got)
	}
}

// A cancel stops a copy with the cancel's error; a shutdown stops it with
// ErrCoordinatorShutdown, which leaves the attempt's outputs to the resume.
func TestSnapshotOutputStopsOnCancelAndShutdown(t *testing.T) {
	source := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(source, []byte("draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	stopping := make(chan struct{})
	close(stopping)
	shutdown, stop := context.WithCancel(WithShutdownFence(context.Background(), stopping))
	stop()
	for ctx, want := range map[context.Context]error{canceled: context.Canceled, shutdown: ErrCoordinatorShutdown} {
		parent := filepath.Join(t.TempDir(), "outputs", "port_work_out")
		if _, err := snapshotOutput(ctx, source, parent, nil); !errors.Is(err, want) {
			t.Fatalf("snapshot = %v, want %v", err, want)
		}
	}
}

// inPlaceFixExecutor fixes a draft that was sent back where it lies, as a seat
// might, and writes its own version in its attempt directory.
type inPlaceFixExecutor struct {
	prompts []string
	inPlace []error
}

func (e *inPlaceFixExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	var b strings.Builder
	renderBriefAndInputs(&b, req, PersonaCard{})
	e.prompts = append(e.prompts, b.String())
	text := fmt.Sprintf("draft %d", req.Attempt)
	if req.Attempt == 2 {
		text += " LINT OK"
	}
	for _, input := range req.Inputs {
		if input.Feedback != nil && input.Feedback.OriginalRef != "" {
			e.inPlace = append(e.inPlace, os.WriteFile(input.Feedback.OriginalRef, []byte(text), 0o600))
		}
	}
	path := filepath.Join(req.ArtifactDir, "draft.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return FormationExecutionResult{}, err
	}
	return FormationExecutionResult{Status: "done", Text: text, Outputs: map[string]FormationOutputPayload{
		"port_work_out": {Text: text, Ref: path},
	}}, nil
}

// An output the seat wrote in its own attempt directory is kept under
// outputs/ too, and the copy a send-back hands the next attempt is read-only:
// the seat that tries to fix it in place cannot, and is told to write its new
// version in its own directory.
func TestKeptOutputsCannotBeRewrittenThroughTheirRoutes(t *testing.T) {
	executor := &inPlaceFixExecutor{}
	store, status := startDraftRun(t, draftLoopBoardFixture(), executor)
	if status.Status != RunStatusSucceeded || len(executor.prompts) != 2 {
		t.Fatalf("status = %+v after %d attempts", status, len(executor.prompts))
	}
	artifacts := filepath.Join(store.Workspace, ".archon", "artifacts", status.RunID, "fmn_work")
	first := filepath.Join(artifacts, "1", "outputs", "port_work_out", "draft.md")
	if len(executor.inPlace) != 1 || os.Geteuid() != 0 && !errors.Is(executor.inPlace[0], fs.ErrPermission) {
		t.Fatalf("in-place fixes = %v, want one refused", executor.inPlace)
	}
	if got := readBody(t, first); got != "draft 1" {
		t.Fatalf("attempt 1's kept output = %q, want draft 1", got)
	}
	for _, path := range []string{first, filepath.Dir(first), filepath.Join(artifacts, "1", "outputs")} {
		readOnly(t, path)
	}
	brief := executor.prompts[1]
	for _, want := range []string{
		"original input ref: " + first + "\n",
		"Input paths are kept copies of earlier work and are read-only; write new versions in your own attempt directory.\n",
		"attempt artifact directory: " + filepath.Join(artifacts, "2") + "\n",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("attempt 2 brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(executor.prompts[0], "kept copies") {
		t.Errorf("attempt 1, handed no kept path, was told about kept copies:\n%s", executor.prompts[0])
	}
	want := []RunOutput{{Name: "report", Kind: PortKindFile, Value: filepath.Join(artifacts, "2", "outputs", "port_work_out", "draft.md"), NodeID: "fmn_work", PortID: "port_work_out", Attempt: 2}}
	if !reflect.DeepEqual(status.Outputs, want) {
		t.Fatalf("run outputs = %+v, want %+v", status.Outputs, want)
	}
}

// Every node_output names its attempt, and every delivery names the output it
// carries (RunInputRef.Origin): the Input card's brief, and on a gate's fail
// route the attempt the gate rejected.
func TestOutputsNameTheirAttemptAlongTheirRoutes(t *testing.T) {
	executor := &rewritingExecutor{path: filepath.Join(t.TempDir(), "draft.md")}
	store, status := startDraftRun(t, draftLoopBoardFixture(), executor)
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v", status)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var outputs, judged []int
	for _, event := range events {
		switch {
		case event.Type == RunEventNodeOutput && event.NodeID == "fmn_work":
			outputs = append(outputs, event.Attempt)
		case event.Type == RunEventNodeStarted && event.NodeID == "fmn_work":
			inputs := runInputRefsFromAny(event.Data["inputRefs"])
			want := OutputOrigin{NodeID: "mis_showcase", PortID: "out"}
			from := "mis_showcase:out"
			if event.Attempt > 1 {
				want, from = OutputOrigin{NodeID: "fmn_work", PortID: "port_work_out", Attempt: event.Attempt - 1}, "gate_lint:fail"
			}
			if len(inputs) != 1 || inputs[0].Origin == nil || *inputs[0].Origin != want || inputs[0].FromNodeID+":"+inputs[0].FromPortID != from {
				t.Fatalf("attempt %d inputs = %+v, want %s carrying %+v", event.Attempt, inputs, from, want)
			}
		case event.Type == RunEventGateEvaluating:
			if origin := runInputRefFromAny(event.Data["inputRef"]).Origin; origin != nil && origin.NodeID == "fmn_work" {
				judged = append(judged, origin.Attempt)
			}
		}
	}
	if want := []int{1, 2, 3}; !reflect.DeepEqual(outputs, want) || !reflect.DeepEqual(judged, want) {
		t.Fatalf("outputs = %v, judged = %v; want %v", outputs, judged, want)
	}
}

// stopAfterSeatsExecutor finishes its seats, then stops the coordinator, as a
// shutdown landing while Archon keeps the attempt's outputs.
type stopAfterSeatsExecutor struct {
	path  string
	stop  func(req FormationExecution)
	calls int
}

func (e *stopAfterSeatsExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	e.calls++
	if err := os.WriteFile(e.path, []byte("draft LINT OK"), 0o600); err != nil {
		return FormationExecutionResult{}, err
	}
	if e.stop != nil {
		e.stop(req)
		e.stop = nil
	}
	return FormationExecutionResult{Status: "done", Text: "draft LINT OK", Outputs: map[string]FormationOutputPayload{
		"port_work_out": {Text: "draft LINT OK", Ref: e.path},
	}}, nil
}

// An attempt whose seats finished but whose outputs were not kept, because a
// shutdown stopped the copy or the copy failed, blocks with its result; the
// resume keeps the outputs then and the run goes on, without running the step
// again. A resume in redispatch mode runs the step again instead.
func TestUnkeptOutputsAreKeptOnResumeWithoutRunningTheStepAgain(t *testing.T) {
	failCopy := func(t *testing.T, _ context.CancelFunc, _ chan struct{}) func(FormationExecution) {
		// The attempt's outputs directory cannot be made.
		return func(req FormationExecution) {
			if err := os.WriteFile(filepath.Join(req.ArtifactDir, attemptOutputsDir), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name, code, mode string
		stop             func(t *testing.T, cancel context.CancelFunc, stopping chan struct{}) func(FormationExecution)
		fix              func(t *testing.T, store *Store, runID string)
		attempt          int
	}{
		{
			name: "shutdown", code: RunBlockCoordinatorShutdown, attempt: 1,
			stop: func(t *testing.T, cancel context.CancelFunc, stopping chan struct{}) func(FormationExecution) {
				return func(FormationExecution) { close(stopping); cancel() }
			},
		},
		{
			name: "copy failure", code: RunBlockOutputNotKept, attempt: 1, stop: failCopy,
			fix: func(t *testing.T, store *Store, runID string) {
				if err := os.Remove(filepath.Join(store.Workspace, ".archon", "artifacts", runID, "fmn_work", "1", attemptOutputsDir)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{name: "redispatch", code: RunBlockOutputNotKept, mode: "redispatch", attempt: 2, stop: failCopy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), draftLoopBoardFixture())
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			stopping := make(chan struct{})
			ctx, cancel := context.WithCancel(WithShutdownFence(context.Background(), stopping))
			defer cancel()
			executor := &stopAfterSeatsExecutor{path: filepath.Join(t.TempDir(), "draft.md")}
			executor.stop = tc.stop(t, cancel, stopping)
			engine := NewRunEngine(store, personas, executor)
			engine.SetGateEvaluator(NewCodeGateEvaluator())
			engine.SetExecutionContext(func(string) context.Context { return ctx })
			status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev})
			if err != nil || status.Status != RunStatusBlocked {
				t.Fatalf("status = %+v, %v; want blocked", status, err)
			}
			block := lastRunEvent(t, store, status.RunID)
			pending := pendingOutputFromEvent(block)
			if block.Type != RunEventBlocked || block.Data["code"] != tc.code || pending == nil || pending.NodeID != "fmn_work" || pending.Attempt != 1 {
				t.Fatalf("block = %+v, want %s carrying attempt 1's result", block, tc.code)
			}
			if tc.fix != nil {
				tc.fix(t, store, status.RunID)
			}
			// A fresh engine, as after the restart.
			resumed := NewRunEngine(store, personas, executor)
			resumed.SetGateEvaluator(NewCodeGateEvaluator())
			status, err = resumed.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Reason: "restarted", Mode: tc.mode})
			if err != nil || status.Status != RunStatusSucceeded {
				t.Fatalf("resumed = %+v, %v; want succeeded", status, err)
			}
			if executor.calls != tc.attempt {
				t.Fatalf("the step ran %d times, want %d", executor.calls, tc.attempt)
			}
			kept := filepath.Join(store.Workspace, ".archon", "artifacts", status.RunID, "fmn_work", strconv.Itoa(tc.attempt), "outputs", "port_work_out", "draft.md")
			if len(status.Outputs) != 1 || status.Outputs[0].Value != kept || status.Outputs[0].Attempt != tc.attempt {
				t.Fatalf("outputs = %+v, want attempt %d's kept draft", status.Outputs, tc.attempt)
			}
			evidence, err := store.ProjectRunAttempts(status.RunID, "fmn_work")
			if err != nil || len(evidence.Steps[0].Attempts) != tc.attempt || evidence.Steps[0].Attempts[tc.attempt-1].Status != AttemptDone {
				t.Fatalf("attempts = %+v, %v", evidence, err)
			}
		})
	}
}
