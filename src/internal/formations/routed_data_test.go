package formations

import (
	"encoding/json"
	"strings"
	"testing"
)

// Routed data reaches the next step exactly as the seat wrote it: secret-shaped
// text is data, and redaction belongs only where Archon displays text to a
// person. An escaped quote after a credential-shaped value must not break the
// payload's JSON.
func TestInlineOutputPayloadsRouteSecretShapedTextVerbatim(t *testing.T) {
	for name, left := range map[string]string{
		"api_key assignment":  "use api_key=abc123 for the staging call",
		"sk token":            "the key is sk-abcdefghijklmnop",
		"password colon":      "password: string is the field type",
		"escaped quote JSON":  `config line: "api_key=abc123" stays quoted`,
		"token after newline": "line one\ntoken=xyz987, then more",
	} {
		t.Run(name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			store.Now = fixedClock()
			personas.Now = fixedClock()
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4NamedOutputBoardFixture())
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			rawPayloads, err := json.Marshal(map[string]FormationOutputPayload{
				"port_split_left":  {Text: left},
				"port_split_right": {Text: "RIGHT"},
			})
			if err != nil {
				t.Fatal(err)
			}
			cfg := tmuxTestConfig(t)
			cfg.Cwd = store.Workspace
			client := &fakeTmuxHarnessClient{
				sessions: []string{"tmux-scout"},
				pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
				captures: []string{
					"splitter\n```archon-outputs\n" + string(rawPayloads) + "\n```\n<<<ARCHON-DONE run-id=run_missing status=ok artifact=split.md>>>",
					"left done\n<<<ARCHON-DONE run-id=run_missing status=ok artifact=left.md>>>",
					"right done\n<<<ARCHON-DONE run-id=run_missing status=ok artifact=right.md>>>",
				},
			}
			executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			status, err := NewRunEngine(store, personas, executor).RunMission("session-search", RunStartRequest{
				MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
				Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 1},
			})
			if err != nil || status.Status != RunStatusSucceeded {
				t.Fatalf("status %+v %v", status, err)
			}
			events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
			if got := firstStartedInputText(t, events, "fmn_left"); got != left {
				t.Fatalf("routed input = %q, want %q", got, left)
			}
			if len(client.sentPrompts) < 2 || !strings.Contains(client.sentPrompts[1], left) {
				t.Fatal("the next seat's brief lost the verbatim payload")
			}
		})
	}
}

// Captured seat text feeds controller plans and peer openings, so it is never
// redacted either.
func TestCapturedSlotTextIsNotRedacted(t *testing.T) {
	captured := "plan: call with api_key=abc123 and sk-abcdefghijklmnop\npassword: hunter2\n<<<ARCHON-DONE run-id=run_x status=ok artifact=a.md>>>"
	got := extractCapturedSlotText(captured, "", "run_x")
	if got != "plan: call with api_key=abc123 and sk-abcdefghijklmnop\npassword: hunter2" {
		t.Fatalf("captured text = %q", got)
	}
}
