package formations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeNativePastedPointerRecognition(t *testing.T) {
	pointer := seatPointer("/work/briefs/critic-second-attempt.md")
	// Native Claude transcript 72f05167-0103-4991-b13a-4d382c71c8a8,
	// 2026-09-19: the human pointer at line 7 used this exact wrapper; its
	// completed critic PASS at line 45 went unconsumed by the adapter.
	wrapped := "\n\n<pasted_content id=\"57da\">\n" + pointer + "\n</pasted_content id=\"57da\">\n"
	tests := []struct {
		name, text, cwd, session, origin string
		meta, want                       bool
	}{
		{"plain", pointer, "/work", "session", "human", false, true},
		{"native paste", wrapped, "/work", "session", "human", false, true},
		{"extra narrative", "Please inspect this:\n" + wrapped, "/work", "session", "human", false, false},
		{"extra trailing text", wrapped + "Do something else", "/work", "session", "human", false, false},
		{"extra pasted text", "\n\n<pasted_content id=\"57da\">\n" + pointer + "\nExtra instructions\n</pasted_content id=\"57da\">\n", "/work", "session", "human", false, false},
		{"mismatched wrapper", "\n\n<pasted_content id=\"57da\">\n" + pointer + "\n</pasted_content id=\"9999\">\n", "/work", "session", "human", false, false},
		{"different pointer", "\n\n<pasted_content id=\"57da\">\n" + seatPointer("/work/briefs/other.md") + "\n</pasted_content id=\"57da\">\n", "/work", "session", "human", false, false},
		{"wrong cwd", wrapped, "/other", "session", "human", false, false},
		{"missing session", wrapped, "/work", "", "human", false, false},
		{"nonhuman", wrapped, "/work", "session", "task", false, false},
		{"meta", wrapped, "/work", "session", "human", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := map[string]any{"type": "user", "uuid": "pointer-message", "origin": map[string]string{"kind": tt.origin}, "cwd": tt.cwd, "sessionId": tt.session, "isMeta": tt.meta, "message": map[string]any{"role": "user", "content": tt.text}}
			assistant := map[string]any{"type": "assistant", "cwd": "/work", "sessionId": "session", "message": map[string]any{"role": "assistant", "model": "claude-opus-5", "stop_reason": "end_turn", "content": []map[string]string{{"type": "text", "text": "<<<CHROTE-DONE run-id=run_fixture status=ok artifact=none>>>"}}}}
			a, _ := json.Marshal(user)
			b, _ := json.Marshal(assistant)
			path := filepath.Join(t.TempDir(), "native.jsonl")
			if err := os.WriteFile(path, append(append(a, '\n'), b...), 0600); err != nil {
				t.Fatal(err)
			}
			seat := &nativeSeat{variant: HarnessVariant{ID: "claude-code"}, runID: fixtureRunID}
			turn, err := readLatestSeatTurn(seat, path, "/work", pointer)
			if err != nil || turn.Consumed != tt.want || turn.Complete != tt.want {
				t.Fatalf("turn %+v error %v, want consumed/complete %v", turn, err, tt.want)
			}
			if tt.want && (turn.SessionID != "session" || turn.TurnID != "pointer-message") {
				t.Fatalf("lost native identity: %+v", turn)
			}
		})
	}
}
