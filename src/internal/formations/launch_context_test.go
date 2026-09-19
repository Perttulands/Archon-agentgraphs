package formations

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchContextSurvivesAdmissionAndResumeIntoEverySeatPrompt(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	paths := []string{t.TempDir(), filepath.Join(t.TempDir(), "prior art.md")}
	if err := os.WriteFile(paths[1], []byte("existing implementation"), 0600); err != nil {
		t.Fatal(err)
	}
	first := &fakeRunExecutor{}
	status, err := NewRunEngine(store, personas, first).RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", ContextPaths: paths, Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(status.ContextPaths, paths) {
		t.Fatalf("projected context: %v", status.ContextPaths)
	}
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	second := &fakeRunExecutor{}
	if _, err := NewRunEngine(store, personas, second).ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach"}); err != nil {
		t.Fatal(err)
	}
	calls := append(first.calls, second.calls...)
	if len(calls) != 2 {
		t.Fatalf("calls: %v", calls)
	}
	for _, call := range calls {
		if !reflect.DeepEqual(call.ContextPaths, paths) {
			t.Fatalf("seat context: %v", call.ContextPaths)
		}
		if call.Cwd == paths[0] {
			t.Fatal("context replaced workspace")
		}
		card, variant, slot := PersonaCard{ID: "scout"}, HarnessVariant{ID: "openai-codex"}, FormationSlot{ID: "worker"}
		for name, prompt := range map[string]string{
			"lab":  (&LabFormationExecutor{config: LabExecutorConfig{Cwd: t.TempDir()}}).renderPrompt(call, slot, card, variant),
			"tmux": (&TmuxFormationExecutor{config: TmuxExecutorConfig{Cwd: t.TempDir()}}).renderPromptWithContext(call, slot, card, variant, "", nil),
		} {
			for _, path := range paths {
				if !strings.Contains(prompt, path) {
					t.Fatalf("%s missing context path %q", name, path)
				}
			}
			if !strings.Contains(prompt, "inspect these files or directories before") {
				t.Fatalf("%s lacks inspection instruction", name)
			}
		}
	}
}

func TestInvalidLaunchContextDoesNotAllocateWorkspace(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	for _, path := range []string{"", "relative", filepath.Join(t.TempDir(), "missing"), os.DevNull} {
		if _, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", ContextPaths: []string{path}, Personas: personas}); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	if _, err := os.Stat(filepath.Join(store.Workspace, "workspaces")); !os.IsNotExist(err) {
		t.Fatalf("allocated workspace: %v", err)
	}
}
