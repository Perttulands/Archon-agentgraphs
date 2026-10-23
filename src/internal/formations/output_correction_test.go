package formations

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Reproduce both AQ failures on text refs, and prove the typed file port
// already accepts the same files without reading their bytes as routed text.
func TestAQOutputRefReproductions(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"overview.jpg-large", strings.Repeat("x", 1200000), "oversized_output_ref"},
		{"progression.jpg-binary", "\xff\xd8\x00\xff", "invalid_output_ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			cfg := tmuxTestConfig(t)
			e := newTmuxFormationExecutorWithClient(store, personas, cfg, &fakeTmuxHarnessClient{})
			path := filepath.Join(t.TempDir(), tc.name)
			writeFixture(t, path, tc.body)
			text := fmt.Sprintf("```archon-outputs\n{\"port_out\":{\"ref\":%q}}\n```", path)
			req := FormationExecution{Formation: FormationNode{Outputs: []FormationPort{{ID: "port_out"}}}}
			_, err := e.formationResultFromText(req, "report", text)
			var execErr *RunExecutionError
			if !errors.As(err, &execErr) || execErr.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
			req.Formation.Outputs[0].Kind = PortKindFile
			result, err := e.formationResultFromText(req, "report", text)
			if err != nil || result.Outputs["port_out"].Ref != path {
				t.Fatalf("file output = %+v, %v", result, err)
			}
		})
	}
}

func TestOutputContractCorrection(t *testing.T) {
	for _, failure := range []string{"missing", "malformed", "unknown", "large", "binary"} {
		for _, twice := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/twice=%t", failure, twice), func(t *testing.T) {
				store, personas := s4RunFixture(t)
				createS4Persona(t, personas, "scout")
				writeFixture(t, store.BoardPath("session-search"), s4NamedOutputBoardFixture())
				board, err := store.ReadBoard("session-search")
				if err != nil {
					t.Fatal(err)
				}
				cfg := tmuxTestConfig(t)
				bad := "work done"
				switch failure {
				case "malformed":
					bad += "\n```archon-outputs\n{broken}\n```"
				case "unknown":
					bad += "\n```archon-outputs\n{\"other\":{\"text\":\"done\"}}\n```"
				case "large", "binary":
					path := filepath.Join(t.TempDir(), "image.jpg")
					body := strings.Repeat("x", 1200000)
					if failure == "binary" {
						body = "\xff\xd8\x00\xff"
					}
					writeFixture(t, path, body)
					bad += fmt.Sprintf("\n```archon-outputs\n{\"port_split_left\":{\"ref\":%q},\"port_split_right\":{\"text\":\"right\"}}\n```", path)
				}
				sentinel := "\n<<<ARCHON-DONE run-id=run_missing status=ok artifact=none>>>"
				good := "```archon-outputs\n{\"port_split_left\":{\"text\":\"/absolute/path/image.jpg\"},\"port_split_right\":{\"text\":\"right\"}}\n```"
				second := good
				if twice {
					second = bad
				}
				client := &fakeTmuxHarnessClient{rawOutputs: true, pane: tmuxPaneState{CurrentPath: cfg.Cwd}, captures: []string{bad + sentinel, second + sentinel},
					seatCaptures: map[string][]string{
						"slot_left":  {"```archon-outputs\n{\"port_left_out\":{\"text\":\"done\"}}\n```" + sentinel},
						"slot_right": {"```archon-outputs\n{\"port_right_out\":{\"text\":\"done\"}}\n```" + sentinel},
					}}
				engine := NewRunEngine(store, personas, newTmuxFormationExecutorWithClient(store, personas, cfg, client))
				status, err := engine.RunMission("session-search", RunStartRequest{Cwd: cfg.Cwd, MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev})
				if err != nil {
					t.Fatal(err)
				}
				if len(client.sentPrompts) < 2 || client.sendTargets[0] != client.sendTargets[1] {
					t.Fatal("no same-seat correction")
				}
				prompt := client.sentPrompts[1]
				for _, want := range []string{"output-correction", "port_split_left", "port_split_right", "omit ref", "preserving the work"} {
					if !strings.Contains(prompt, want) {
						t.Fatalf("correction missing %q", want)
					}
				}
				if twice {
					if status.Status != RunStatusBlocked || len(client.sentPrompts) != 2 {
						t.Fatalf("status=%s turns=%d", status.Status, len(client.sentPrompts))
					}
				} else {
					if status.Status != RunStatusSucceeded {
						events, _ := store.ReadRunEvents(status.RunID)
						t.Fatalf("status=%s events=%+v", status.Status, events)
					}
					events, _ := store.ReadRunEvents(status.RunID)
					if got := firstStartedInputText(t, events, "fmn_left"); got != "/absolute/path/image.jpg" {
						t.Fatalf("routed %q", got)
					}
				}
			})
		}
	}
}

// The original forgotten-fence report was a final judge with a valid verdict.
// Its repair must preserve that verdict so the gate routes without another run.
func TestJudgeCorrectsMissingOutputsWithoutLosingVerdict(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
	writeFixture(t, store.BoardPath("session-search"), fixture)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := "\n<<<ARCHON-DONE run-id=run_missing status=ok artifact=none>>>"
	output := func(port string) string {
		return fmt.Sprintf("\n```archon-outputs\n{%q:{\"text\":\"done\"}}\n```", port)
	}
	verdict := judgeBlock("pass", "covered")
	cfg := tmuxTestConfig(t)
	client := &fakeTmuxHarnessClient{rawOutputs: true, pane: tmuxPaneState{CurrentPath: cfg.Cwd}, captures: []string{
		"draft" + output("port_work_out") + sentinel,
		"review" + output("port_j1_out") + sentinel,
		verdict + sentinel,
		verdict + output("port_j2_out") + sentinel,
		"shipped" + output("port_ship_out") + sentinel,
	}}
	engine := NewRunEngine(store, personas, newTmuxFormationExecutorWithClient(store, personas, cfg, client))
	status, err := engine.RunMission("session-search", RunStartRequest{Cwd: cfg.Cwd, MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if len(client.sentPrompts) != 5 || client.sendTargets[2] != client.sendTargets[3] {
		t.Fatal("judge repair did not reuse seat")
	}
	if !strings.Contains(client.sentPrompts[3], judgeContract) || !strings.Contains(client.sentPrompts[3], "port_j2_out") {
		t.Fatal("repair omitted judge or output contract")
	}
}
