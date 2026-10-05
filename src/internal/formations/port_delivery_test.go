package formations

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// A pass applies to what the operator saw, even if another delivery leaves
// newer unread work on the port before its next dispatch (archon-qtq1).
func TestHumanResponsesIdentifyApprovedWorkAfterUnreadPortMerge(t *testing.T) {
	first := humanPassedPortInput(t, "review_first", "draft v1", "ledger://run/work/1", "  Hyväksy v1.\r\n")
	second := humanPassedPortInput(t, "review_second", "draft v2", "ledger://run/work/2", "Approve v2 only.\n")
	newWork := RunInputRef{Ref: "ledger://run/work/3", Text: "draft v3: not shown to the operator"}
	for _, test := range []struct {
		name       string
		deliveries []RunInputRef
		approvals  []RunInputRef
	}{
		{"new work after two approvals", []RunInputRef{first, second, newWork}, []RunInputRef{second, first}},
		// A later send-back retains newer unread work and carries the approval
		// of the old work with it, as when a downstream gate returns that work.
		{"old approval returned onto new work", []RunInputRef{newWork, gateFailInput("run", BoardConnection{ID: "return"}, "rework", 1, first, "revise the old branch", nil)}, []RunInputRef{first}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ready := map[string]map[string]RunInputRef{}
			for _, delivery := range test.deliveries {
				deliverToPort(ready, "worker", "in", delivery)
			}
			merged := ready["worker"]["in"]
			if merged.Text != newWork.Text || merged.Ref != newWork.Ref {
				t.Fatalf("port work = %+v, want the newer unapproved work", merged)
			}
			decoded := runInputRefFromAny(portInputJSON(t, merged))
			if !reflect.DeepEqual(decoded.Response, merged.Response) {
				t.Fatalf("durable response chain = %+v, want %+v", decoded.Response, merged.Response)
			}
			for state, input := range map[string]RunInputRef{"live": merged, "durable": decoded} {
				req := FormationExecution{RunID: "run", Brief: FormationBrief{Goal: "use the latest work and keep each approval in context"}, Inputs: []RunInputRef{input}}
				card := PersonaCard{ID: "builder"}
				variant := HarnessVariant{ID: "openai-codex"}
				for executor, prompt := range map[string]string{
					"lab":  (&LabFormationExecutor{config: LabExecutorConfig{Cwd: t.TempDir()}}).renderPrompt(req, FormationSlot{ID: "worker"}, card, variant),
					"tmux": (&TmuxFormationExecutor{config: TmuxExecutorConfig{Cwd: t.TempDir()}}).renderPromptWithContext(req, FormationSlot{ID: "worker"}, card, variant, "", nil),
				} {
					if !strings.Contains(prompt, "input: "+newWork.Text+"\n") || strings.Contains(prompt, "approved input: "+newWork.Text) {
						t.Errorf("%s %s prompt conflates newer work with the approval:\n%s", state, executor, prompt)
					}
					for _, approved := range test.approvals {
						section := "human response from " + approved.Response.GateID + ", attempt 1:\nverdict: pass\ndecided by: human:operator\nresponse:\n" + approved.Response.Text + "\napproved input ref: " + approved.Ref + "\napproved input: " + approved.Text + "\n"
						if !strings.Contains(prompt, section) {
							t.Errorf("%s %s prompt does not identify the work approved by %s:\n%s", state, executor, approved.Response.GateID, prompt)
						}
					}
				}
			}
		})
	}
}

// Reconstruct through the same exact-request ledger rule as a real pass;
// don't prefill the response context that this regression must establish.
func humanPassedPortInput(t *testing.T, gateID, text, ref, answer string) RunInputRef {
	t.Helper()
	input := RunInputRef{Text: text, Ref: ref}
	events := []RunEvent{
		{Seq: 1, Type: RunEventHumanInputRequested, GateID: gateID, Data: map[string]any{"inputRef": portInputJSON(t, input)}},
		{Seq: 2, Type: RunEventHumanVerdictRecorded, GateID: gateID, Data: map[string]any{"requestedSeq": 1, "verdict": "pass", "reason": answer, "decidedBy": "human:operator"}},
		{Seq: 3, Type: RunEventGateVerdict, GateID: gateID, Attempt: 1, Data: map[string]any{"requestedSeq": 1}},
	}
	passed, err := gatePassInput(events, events[2], gateID, input)
	if err != nil || passed.Response == nil {
		t.Fatalf("gate pass = %+v, %v", passed, err)
	}
	return passed
}

func portInputJSON(t *testing.T, input RunInputRef) map[string]any {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
