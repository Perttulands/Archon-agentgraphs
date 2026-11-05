package formations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These records and screens are written by hand after seats of Pi 1.5.0, with
// only what the tests read. See testdata/README.md.
const (
	piTestPointer = "Read the file /work/briefs/seat-1.md and execute it exactly; it is your whole brief."
	piTestHeader  = `{"type":"session","version":3,"id":"fixture-session","timestamp":"2026-11-05T10:00:00.000Z","cwd":"/work"}`
	piTestModel   = `{"type":"model_change","id":"e1","parentId":null,"provider":"anthropic","modelId":"claude-opus-5-5"}`
	piTestLevel   = `{"type":"thinking_level_change","id":"e2","parentId":"e1","thinkingLevel":"high"}`
	piTestUser    = `{"type":"message","id":"e3","parentId":"e2","message":{"role":"user","content":[{"type":"text","text":"` + piTestPointer + `"}]}}`
)

func piAssistant(text, stop string) string {
	return `{"type":"message","id":"e4","parentId":"e3","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"` + text + `"}],"provider":"anthropic","model":"claude-opus-5-5","usage":{"input":10,"output":20,"cacheRead":100,"cacheWrite":5},"stopReason":"` + stop + `"}}`
}

func readPiFixture(t *testing.T, lines ...string) (codexTranscriptTurn, error) {
	t.Helper()
	return readPiTurnReader(strings.NewReader(strings.Join(lines, "\n")+"\n"), "/work", piTestPointer, "run_1")
}

const piTestDone = "done <<<ARCHON-DONE run-id=run_1 status=ok artifact=none>>>"

func TestPiTurnCompletesOnlyOnNativeStopWithThisRunsSentinel(t *testing.T) {
	head := []string{piTestHeader, piTestModel, piTestLevel, piTestUser}
	turn, err := readPiFixture(t, append(head, piAssistant(piTestDone, "stop"))...)
	if err != nil || !turn.Complete || turn.SessionID != "fixture-session" || turn.Model != "anthropic/claude-opus-5-5" || turn.Effort != "high" {
		t.Fatalf("turn = %+v, err = %v", turn, err)
	}
	for _, stop := range []string{"toolUse", "length", "error", "aborted"} {
		if turn, err := readPiFixture(t, append(head, piAssistant(piTestDone, stop))...); err != nil || turn.Complete {
			t.Fatalf("stopReason %s: turn = %+v, err = %v", stop, turn, err)
		}
	}
	turn, err = readPiFixture(t, append(head, piAssistant("no sentinel", "stop"))...)
	if err != nil || turn.Complete || turn.Waiting != SeatStateTurnEndedWithoutSentinel {
		t.Fatalf("turn = %+v, err = %v", turn, err)
	}
	other := "done <<<ARCHON-DONE run-id=run_2 status=ok artifact=none>>>"
	if turn, err := readPiFixture(t, append(head, piAssistant(other, "stop"))...); err != nil || turn.Complete {
		t.Fatalf("another run's sentinel: turn = %+v, err = %v", turn, err)
	}
}

func TestPiTurnRefusesOtherCwdAndNamesChanges(t *testing.T) {
	other := strings.Replace(piTestHeader, `"/work"`, `"/elsewhere"`, 1)
	if turn, err := readPiFixture(t, other, piTestUser, piAssistant(piTestDone, "stop")); err != nil || turn.Consumed {
		t.Fatalf("other cwd: turn = %+v, err = %v", turn, err)
	}
	head := []string{piTestHeader, piTestModel, piTestLevel, piTestUser}
	changed := `{"type":"model_change","id":"e9","provider":"openai","modelId":"gpt-6"}`
	if _, err := readPiFixture(t, append(head, changed)...); err == nil || !strings.Contains(err.Error(), "model changed") {
		t.Fatalf("model change: err = %v", err)
	}
	level := `{"type":"thinking_level_change","id":"e9","thinkingLevel":"low"}`
	if _, err := readPiFixture(t, append(head, level)...); err == nil || !strings.Contains(err.Error(), "effort changed") {
		t.Fatalf("effort change: err = %v", err)
	}
}

func TestPiOperatorTurnsNeitherCompleteNorFail(t *testing.T) {
	typed := `{"type":"message","id":"e8","message":{"role":"user","content":"look at this too"}}`
	turn, err := readPiFixture(t, piTestHeader, piTestUser, typed, piAssistant(piTestDone, "stop"))
	if err != nil || !turn.Complete || turn.OperatorTurns != 1 {
		t.Fatalf("turn = %+v, err = %v", turn, err)
	}
}

func TestPiModelObserved(t *testing.T) {
	const observed = "anthropic/claude-opus-5-5"
	for _, want := range []string{observed, "claude-opus-5-5", "opus", "anthropic/opus"} {
		if !(HarnessVariant{ID: piHarnessID, Model: want}).modelObserved(observed) {
			t.Errorf("model %q should match %s", want, observed)
		}
	}
	for _, want := range []string{"openai/gpt-6", "openrouter/opus", "sonnet", "anthropic/claude-sonnet-5-5"} {
		if (HarnessVariant{ID: piHarnessID, Model: want}).modelObserved(observed) {
			t.Errorf("model %q should not match %s", want, observed)
		}
	}
}

func TestPiLaunchNamesModelAndThinkingAndNoCredential(t *testing.T) {
	v := HarnessVariant{ID: piHarnessID, Model: "anthropic/claude-opus-5-5", Effort: "xhigh"}
	got, err := v.RenderLaunch("/bin/pi")
	want := "exec '/bin/pi' --model 'anthropic/claude-opus-5-5' --thinking 'xhigh'"
	if err != nil || got != want {
		t.Fatalf("launch = %q, err = %v; want %q", got, err, want)
	}
}

func TestPiSessionDirIsPisOwnLayout(t *testing.T) {
	got := piSessionDir("/home/a/.pi/agent/sessions", "/srv/my:proj")
	if want := "/home/a/.pi/agent/sessions/--srv-my-proj--"; got != want {
		t.Fatalf("session dir = %q, want %q", got, want)
	}
}

// A virtual model's messages name the physical model that answered: the slot's
// model is the one selected, and only another selection changes it.
func TestPiTurnKeepsTheSelectedModel(t *testing.T) {
	virtual := `{"type":"model_change","id":"e1","provider":"router","modelId":"auto"}`
	turn, err := readPiFixture(t, piTestHeader, virtual, piTestUser, piAssistant(piTestDone, "stop"))
	if err != nil || !turn.Complete || turn.Model != "router/auto" {
		t.Fatalf("turn = %+v, err = %v", turn, err)
	}
}

func TestPiDispatchTokensCountFromLatestPointer(t *testing.T) {
	path := t.TempDir() + "/session.jsonl"
	lines := []string{piTestHeader, piTestUser, piAssistant("first", "stop"), piTestUser, piAssistant("a", "toolUse"), piAssistant(piTestDone, "stop"),
		`{"type":"compaction","id":"e7","summary":"s","usage":{"input":1,"output":2,"cacheRead":3,"cacheWrite":4}}`}
	writeFileRef(t, path, strings.Join(lines, "\n")+"\n")
	usage, err := dispatchTokens(piHarnessID, path, piTestPointer)
	want := TokenUsage{Input: 21, CacheWrite: 14, CacheRead: 203, Output: 42}
	if err != nil || usage != want {
		t.Fatalf("usage = %+v, err = %v; want %+v", usage, err, want)
	}
}

func piScreenWith(top string, editor ...string) string {
	rule := strings.Repeat("─", 40)
	return strings.Join(append(append([]string{"earlier answer", "", top}, editor...), rule, "/work", "0.2%/1.0M (auto)            claude-opus-5-5 • high"), "\n")
}

func piScreen(editor ...string) string {
	return piScreenWith(strings.Repeat("─", 40), editor...)
}

func TestPiSeatsAcceptTheEffortsTheirModelsDo(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		if err := validateRunSettings("slot", piHarnessID, "anthropic/claude-opus-5-5", effort); err != nil {
			t.Errorf("effort %s: %v", effort, err)
		}
	}
	if err := validateRunSettings("slot", piHarnessID, "openai/some-new-model", "ultra"); err == nil {
		t.Error("ultra must be refused")
	}
}

func TestPiSeatLaunchTrustsItsFolderForThisRunOnly(t *testing.T) {
	bin := t.TempDir() + "/pi"
	writeFileRef(t, bin, "#!/bin/sh\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin))
	got, err := HarnessVariant{ID: piHarnessID, Effort: "low"}.SeatLaunchCommand("/work")
	if err != nil || !strings.HasSuffix(got, " --thinking 'low' --approve") || strings.Contains(got, "--no-session") {
		t.Fatalf("seat launch = %q, err = %v", got, err)
	}
}

func TestPiEditorReadsWhatItHolds(t *testing.T) {
	if !seatInputClear(piHarnessID, piScreen("\x1b[7m \x1b[0m"), 0, 0) {
		t.Fatal("empty editor must be clear")
	}
	if seatInputClear(piHarnessID, piScreen("half a thought"), 0, 0) {
		t.Fatal("text in the editor must not be clear")
	}
	typed, found := seatTypedText(piHarnessID, piScreen("Read the file /work/briefs/", "seat-1.md and execute"), 0, 0)
	if !found || typed != renderedText("Read the file /work/briefs/seat-1.md and execute") {
		t.Fatalf("typed = %q, found = %v", typed, found)
	}
	scrolled := piScreenWith("─── ↑ 2 more ───────────", "still typing")
	if typed, found := seatTypedText(piHarnessID, scrolled, 0, 0); !found || typed != "stilltyping" {
		t.Fatalf("scrolled editor typed = %q, found = %v", typed, found)
	}
	if !tmuxPaneShowsHarnessReady(piHarnessID, piScreen("")) {
		t.Fatal("an idle editor is ready")
	}
	if tmuxPaneShowsHarnessReady(piHarnessID, "starting up\n") {
		t.Fatal("no editor is not ready")
	}
	// A rule the agent's answer drew is not the editor.
	answer := "─────────\nanswer below a rule\n" + piScreen("")
	if !seatInputClear(piHarnessID, answer, 0, 0) {
		t.Fatal("a rule in the answer must not hide the empty editor")
	}
}

func TestPiStatusInTheRuleMeansWorking(t *testing.T) {
	for _, top := range []string{
		"── ⠋ Working ─────────────────",
		"── ⠋ Retrying (1/3) in 2s... (escape to cancel) ─────",
		"── ⠋ Auto-compacting... (escape to cancel) ─────",
		"─────── ⠋ ─────",
	} {
		screen := piScreenWith(top, "")
		if seatInputClear(piHarnessID, screen, 0, 0) || tmuxPaneShowsHarnessReady(piHarnessID, screen) {
			t.Fatalf("%q: a working seat is neither clear nor ready", top)
		}
	}
}

// A dialog such as Pi's trust prompt or /model draws its own rules: its text
// is never an empty input line, so nothing is pasted into it.
func TestPiDialogIsNotAnEmptyEditor(t *testing.T) {
	if seatInputClear(piHarnessID, piScreen(" Trust project folder?", " → Trust", "   Do not trust"), 0, 0) {
		t.Fatal("a dialog must not be clear")
	}
}
