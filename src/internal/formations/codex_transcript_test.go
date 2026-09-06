package formations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexTurnUsesExactUserPointerAndNativeCompletion(t *testing.T) {
	base := `{"type":"session_meta","payload":{"id":"native-one","cwd":"/workspace"}}
{"type":"turn_context","payload":{"model":"gpt-6-astra","effort":"xhigh"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"pointer"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","channel":"final","content":[{"text":"first final"}]}}
`
	complete := `{"type":"event_msg","payload":{"type":"task_complete"}}
`
	for _, tt := range []struct {
		name, extra, pointer, cwd string
		consumed, done, wantErr   bool
	}{
		{"unclosed", "", "pointer", "/workspace", true, false, false},
		{"closed", complete, "pointer", "/workspace", true, true, false},
		{"wrong pointer", complete, "different", "/workspace", false, false, false},
		{"wrong workspace", complete, "pointer", "/elsewhere", false, false, false},
		{"later user", `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"another task"}]}}` + "\n" + complete, "pointer", "/workspace", true, false, true},
		{"closed before later turn", complete + `{"type":"turn_context","payload":{"model":"another-model","effort":"low"}}` + "\n", "pointer", "/workspace", true, true, false},
		{"partial final event", strings.TrimSuffix(complete, "}}\n"), "pointer", "/workspace", true, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			os.WriteFile(path, []byte(base+tt.extra), 0600)
			turn, err := readCodexTurn(path, tt.cwd, tt.pointer)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err %v", err)
			}
			if turn.Consumed != tt.consumed || turn.Complete != tt.done {
				t.Fatalf("turn %+v", turn)
			}
			if turn.Complete && (turn.Text != "first final" || turn.Model != "gpt-6-astra" || turn.Effort != "xhigh") {
				t.Fatalf("later turn replaced exact result: %+v", turn)
			}
		})
	}
}
