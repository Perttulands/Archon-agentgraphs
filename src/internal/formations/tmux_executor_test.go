package formations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConfiguredFormationExecutorFromEnvSelectsTmuxOnlyWhenHarnessesSet(t *testing.T) {
	clearExecutorEnv(t)

	executor := NewConfiguredFormationExecutorFromEnv(nil, nil, "test-boundary")
	if _, ok := executor.(unavailableFormationExecutor); !ok {
		t.Fatalf("executor without harness env = %T, want unavailableFormationExecutor", executor)
	}

	t.Setenv("CHROTE_FORMATIONS_TMUX_HARNESSES", "openai-codex")
	executor = NewConfiguredFormationExecutorFromEnv(nil, nil, "test-boundary")
	if _, ok := executor.(*TmuxFormationExecutor); !ok {
		t.Fatalf("executor with tmux harness env = %T, want *TmuxFormationExecutor", executor)
	}

	t.Setenv("CHROTE_FORMATIONS_TMUX_HARNESSES", "")
	t.Setenv("CHROTE_FORMATIONS_LAB_HARNESSES", "openai-codex")
	executor = NewConfiguredFormationExecutorFromEnv(nil, nil, "test-boundary")
	if _, ok := executor.(*TmuxFormationExecutor); ok {
		t.Fatalf("executor with only lab harness env = %T, must not select tmux", executor)
	}
	if _, ok := executor.(*LabFormationExecutor); !ok {
		t.Fatalf("executor with lab harness env = %T, want *LabFormationExecutor", executor)
	}
}

func TestTmuxExecutorAcceptsConfiguredCockpitSocket(t *testing.T) {
	// Owner ruling: Formations shares the cockpit socket. A real, stable,
	// non-symlink socket that is ALSO the configured cockpit socket must now
	// validate cleanly; safety moved from socket-refusal to session-scoping.
	cfg := tmuxTestConfig(t)
	t.Setenv("CHROTE_TERMINAL_USER_SOCKETS", "alice="+cfg.Socket)
	t.Setenv("CHROTE_DEFAULT_TMUX_SOCKET", cfg.Socket)

	if err := newTmuxFormationExecutorWithClient(nil, nil, cfg, &fakeTmuxHarnessClient{}).validateConfiguredBoundary(); err != nil {
		t.Fatalf("configured cockpit socket validate error = %v, want accepted", err)
	}
}

func TestTmuxExecutorAllowsDisposableTempDogfood(t *testing.T) {
	if err := newTmuxFormationExecutorWithClient(nil, nil, tmuxTestConfig(t), &fakeTmuxHarnessClient{}).validateConfiguredBoundary(); err != nil {
		t.Fatalf("disposable temp dogfood validate error = %v, want allowed", err)
	}
}

func TestTmuxExecutorAcceptsProductionDedicatedSocketOutsideTemp(t *testing.T) {
	// Production-style config: a dedicated tmux socket and workspace cwd/roots
	// that live OUTSIDE /tmp. The executor must accept it now that the disposable
	// /tmp dogfood requirement is removed.
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("CHROTE_DEFAULT_TMUX_SOCKET", "")
	t.Setenv("CHROTE_TERMINAL_USER_SOCKETS", "")

	cfg := nonTempWorkspace(t)
	if err := newTmuxFormationExecutorWithClient(nil, nil, cfg, &fakeTmuxHarnessClient{}).validateConfiguredBoundary(); err != nil {
		t.Fatalf("production dedicated socket + workspace outside /tmp validate error = %v, want accepted", err)
	}
}

func TestTmuxExecutorRejectsSocketSymlink(t *testing.T) {
	// The socket-identity/stability check is retained: a socket that is a
	// symlink is refused so the executor cannot be pointed at a moving target.
	cfg := tmuxTestConfig(t)
	aliasSocket := filepath.Join(filepath.Dir(cfg.Socket), "outside.sock")
	if err := os.Symlink("/dev/null", aliasSocket); err != nil {
		t.Fatalf("symlink outside socket fixture: %v", err)
	}
	cfg.Socket = aliasSocket
	assertAttachmentAuditUnavailable(t, cfg)
}

func TestTmuxExecutorNonTempWorkspaceUsesOrdinaryValidationCodes(t *testing.T) {
	// The /tmp workspace boundary is removed: a missing non-/tmp cwd or root is
	// now an ordinary configuration error, not session_target_attachment_audit_unavailable.
	t.Run("missing cwd", func(t *testing.T) {
		cfg := tmuxTestConfig(t)
		cfg.Cwd = "/nonexistent-test-root/path-that-does-not-exist"
		cfg.Roots = []string{cfg.Cwd}
		assertBoundaryCode(t, cfg, "unavailable_cwd")
	})

	t.Run("missing root", func(t *testing.T) {
		cfg := tmuxTestConfig(t)
		cfg.Roots = []string{cfg.Cwd, "/nonexistent-test-root/path-that-does-not-exist"}
		assertBoundaryCode(t, cfg, "unavailable_root")
	})
}

func TestTmuxExecutorRefusesSocketRetargetBeforeSend(t *testing.T) {
	cfg := tmuxTestConfig(t)
	client := &fakeTmuxHarnessClient{
		sessions: []string{"tmux-scout"},
		pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
	}
	client.afterCapture = func(call int) {
		if call != 1 {
			return
		}
		if err := os.Remove(cfg.Socket); err != nil {
			t.Fatalf("remove disposable socket fixture: %v", err)
		}
		if err := os.Symlink("/dev/null", cfg.Socket); err != nil {
			t.Fatalf("retarget disposable socket fixture: %v", err)
		}
	}

	status, events := runTmuxFormationForTestWithConfig(t, client, "", cfg)
	if status.Status != RunStatusBlocked {
		t.Fatalf("run status = %+v, want blocked", status)
	}
	errorEvent := eventOfType(t, events, RunEventError)
	if errorEvent.Data["code"] != "session_target_attachment_audit_unavailable" {
		t.Fatalf("run error code = %#v, want session_target_attachment_audit_unavailable", errorEvent.Data["code"])
	}
	if client.listCalls != 1 || client.describeCalls != 1 || client.captureCalls != 1 {
		t.Fatalf("pre-retarget client calls = list:%d describe:%d capture:%d, want 1/1/1", client.listCalls, client.describeCalls, client.captureCalls)
	}
	if client.sendCalls != 0 {
		t.Fatalf("send calls after socket retarget = %d, want zero", client.sendCalls)
	}
}

func assertAttachmentAuditUnavailable(t *testing.T, cfg TmuxExecutorConfig) {
	t.Helper()
	err := newTmuxFormationExecutorWithClient(nil, nil, cfg, &fakeTmuxHarnessClient{}).validateConfiguredBoundary()
	var executionErr *RunExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("disposable boundary error = %v, want RunExecutionError", err)
	}
	if executionErr.Code != "session_target_attachment_audit_unavailable" {
		t.Fatalf("disposable boundary code = %q, want session_target_attachment_audit_unavailable", executionErr.Code)
	}
}

func assertBoundaryCode(t *testing.T, cfg TmuxExecutorConfig, wantCode string) {
	t.Helper()
	err := newTmuxFormationExecutorWithClient(nil, nil, cfg, &fakeTmuxHarnessClient{}).validateConfiguredBoundary()
	var executionErr *RunExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("boundary error = %v, want RunExecutionError with code %q", err, wantCode)
	}
	if executionErr.Code != wantCode {
		t.Fatalf("boundary code = %q, want %q", executionErr.Code, wantCode)
	}
}

func TestTmuxExecutorSessionFailuresRecordDurableBoundaryAndProvenance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		client   *fakeTmuxHarnessClient
		wantCode string
	}{
		{
			name:     "spawn failure",
			client:   &fakeTmuxHarnessClient{createErr: errors.New("tmux new-session refused")},
			wantCode: "session_spawn_failed",
		},
		{
			name:     "dead pane",
			client:   &fakeTmuxHarnessClient{pane: tmuxPaneState{Dead: true}},
			wantCode: "dead_pane",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, events := runTmuxFormationForTest(t, tc.client, "")
			if status.Status != RunStatusBlocked || !status.ResumeAllowed {
				t.Fatalf("status = %+v, want resumable blocked run", status)
			}
			errEvent := eventOfType(t, events, RunEventError)
			if errEvent.NodeID != "fmn_research" || errEvent.SlotID != "slot_research" {
				t.Fatalf("error provenance envelope = %+v, want node/slot", errEvent)
			}
			if errEvent.Data["code"] != tc.wantCode || errEvent.Data["boundary"] != "adapter" {
				t.Fatalf("error data = %#v, want code %s boundary adapter", errEvent.Data, tc.wantCode)
			}
			if errEvent.Data["nodeId"] != "fmn_research" || errEvent.Data["slotId"] != "slot_research" || errEvent.Data["recoverable"] != true {
				t.Fatalf("error data provenance = %#v, want nodeId/slotId/recoverable", errEvent.Data)
			}
			blocked := eventOfType(t, events, RunEventBlocked)
			if blocked.NodeID != "fmn_research" || blocked.SlotID != "slot_research" || blocked.Data["blockedNodeId"] != "fmn_research" {
				t.Fatalf("blocked event = %+v data=%#v, want durable blocked node/slot", blocked, blocked.Data)
			}
			if tc.client.sendCalls != 0 {
				t.Fatalf("send calls = %d, want no send after unsafe session state", tc.client.sendCalls)
			}
		})
	}
}

func TestTmuxExecutorWaitsForCodexTUIReadinessBeforeDispatch(t *testing.T) {
	client := &fakeTmuxHarnessClient{
		startupCaptures: []string{
			"OpenAI Codex\nstarting",
			"OpenAI Codex\n› Explain this codebase",
		},
	}
	status, _ := runTmuxFormationForTest(t, client, "")
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	if client.sendCalls != 1 {
		t.Fatalf("send calls = %d, want one", client.sendCalls)
	}
	if client.preSendCaptureCalls < 2 {
		t.Fatalf("pre-send captures = %d, want at least two so dispatch waits past startup", client.preSendCaptureCalls)
	}
	if !strings.Contains(client.lastPreSendCapture, "› Explain this codebase") {
		t.Fatalf("last pre-send capture = %q, want initialized Codex input before dispatch", client.lastPreSendCapture)
	}
}

func TestTmuxPaneShowsHarnessReadyIgnoresTrailingBlankRows(t *testing.T) {
	for _, tc := range []struct {
		name      string
		harnessID string
		captured  string
	}{
		{
			name:      "Codex",
			harnessID: "openai-codex",
			captured:  "OpenAI Codex\n› Implement {feature}\n\n  gpt-5.6-sol xhigh · ~" + strings.Repeat("\n", 10),
		},
		{
			name:      "Claude",
			harnessID: "claude-code",
			captured:  "Claude Code\n❯" + strings.Repeat("\n", 10),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tmuxPaneShowsHarnessReady(tc.harnessID, tc.captured) {
				t.Fatalf("matcher rejected initialized %s pane with trailing blank rows", tc.name)
			}
		})
	}
}

func TestTmuxPaneShowsHarnessReadyRejectsClaudeTrustPrompt(t *testing.T) {
	captured := strings.Join([]string{
		"Quick safety check: Is this a project you created or one you trust?",
		"Claude Code'll be able to read, edit, and execute files here.",
		"❯ 1. Yes, I trust this folder",
		"  2. No, exit",
	}, "\n")
	if tmuxPaneShowsHarnessReady("claude-code", captured) {
		t.Fatal("Claude workspace trust prompt was misread as an idle agent input")
	}
}

func TestTmuxExecutorWaitsForClaudeTUIReadinessBeforeDispatch(t *testing.T) {
	cfg := tmuxTestConfig(t)

	client := &fakeTmuxHarnessClient{
		harness: "claude-code",
		startupCaptures: []string{
			"Claude Code\nstarting",
			"Claude Code\n❯",
		},
	}

	status, _ := runTmuxFormationForTestWithConfig(t, client, "", cfg)
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	if client.sendCalls != 1 {
		t.Fatalf("send calls = %d, want one", client.sendCalls)
	}
	if client.preSendCaptureCalls < 2 {
		t.Fatalf("pre-send captures = %d, want at least two so dispatch waits past startup", client.preSendCaptureCalls)
	}
	if !strings.Contains(client.lastPreSendCapture, "❯") {
		t.Fatalf("last pre-send capture = %q, want initialized Claude input before dispatch", client.lastPreSendCapture)
	}
}

func TestTmuxExecutorNeverDispatchesWhenClaudeTUIReadinessUnknown(t *testing.T) {
	client := &fakeTmuxHarnessClient{
		harness:         "claude-code",
		startupCaptures: []string{"Claude Code\nstarting"},
	}
	status, events := runTmuxFormationForTest(t, client, "")
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status = %+v, want resumable blocked run", status)
	}
	if client.sendCalls != 0 {
		t.Fatalf("send calls = %d, want zero before readiness", client.sendCalls)
	}
	if eventsContainType(events, RunEventSlotDispatch) || eventsContainType(events, RunEventAdapterSend) {
		t.Fatalf("events = %v, want no slot_dispatch or adapter_send before readiness", eventTypes(events))
	}
	errEvent := eventOfType(t, events, RunEventError)
	if errEvent.Data["code"] != "session_startup_timeout" || errEvent.Data["boundary"] != "adapter" {
		t.Fatalf("error data = %#v, want session_startup_timeout at adapter boundary", errEvent.Data)
	}
	if len(client.created) != 1 || fmt.Sprint(client.killed) != fmt.Sprint(client.created) {
		t.Fatalf("created=%v killed=%v, want exact owned-session cleanup", client.created, client.killed)
	}
}

func TestTmuxExecutorNeverDispatchesWhenCodexTUIReadinessUnknown(t *testing.T) {
	client := &fakeTmuxHarnessClient{startupCaptures: []string{"OpenAI Codex\nstarting"}}
	status, events := runTmuxFormationForTest(t, client, "")
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status = %+v, want resumable blocked run", status)
	}
	if client.sendCalls != 0 {
		t.Fatalf("send calls = %d, want zero before readiness", client.sendCalls)
	}
	if eventsContainType(events, RunEventSlotDispatch) || eventsContainType(events, RunEventAdapterSend) {
		t.Fatalf("events = %v, want no slot_dispatch or adapter_send before readiness", eventTypes(events))
	}
	errEvent := eventOfType(t, events, RunEventError)
	if errEvent.Data["code"] != "session_startup_timeout" || errEvent.Data["boundary"] != "adapter" {
		t.Fatalf("error data = %#v, want session_startup_timeout at adapter boundary", errEvent.Data)
	}
	if len(client.created) != 1 || fmt.Sprint(client.killed) != fmt.Sprint(client.created) {
		t.Fatalf("created=%v killed=%v, want exact owned-session cleanup", client.created, client.killed)
	}
}

func TestRedactPromptAndSecretTokensFromDispatchAdapterFailureLedger(t *testing.T) {
	store, started := startS4DispatchRun(t)
	prompt := "brief: RAW-PROMPT-DISPATCH-DO-NOT-LOG api_key=sk-dispatchsecret123\ninput: hidden dispatch payload"
	dispatcher := NewSlotDispatcher(store, promptEchoDispatchAdapter{})

	_, err := dispatcher.DispatchSlot(started.RunID, SlotDispatchRequest{
		NodeID:      "fmn_work",
		SlotID:      "slot_work",
		AgentID:     "scout",
		Harness:     "openai-codex",
		SessionStem: "scout",
		SessionRef:  "tmux:scout",
		Prompt:      prompt,
		Attempt:     1,
	})
	if err == nil {
		t.Fatal("dispatch adapter failure returned nil error")
	}

	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	assertLedgerDataForTypesRedacted(t, events, []string{RunEventError, RunEventBlocked},
		"RAW-PROMPT-DISPATCH-DO-NOT-LOG", "hidden dispatch payload", "sk-dispatchsecret123", "sk-dispatchadapter123")
}

func TestTmuxPromptAndSecretsRedactedWhenSendOrCaptureFails(t *testing.T) {
	goal := "RAW-PROMPT-TMUX-DO-NOT-LOG api_key=REDACTED_TEST_SECRET"
	board := tmuxRunBoardWithBrief(goal)

	t.Run("send failure", func(t *testing.T) {
		client := &fakeTmuxHarnessClient{
			sessions:          []string{"tmux-scout"},
			sendErrEchoPrompt: true,
		}
		status, events := runTmuxFormationForTest(t, client, board)
		if status.Status != RunStatusBlocked {
			t.Fatalf("status = %+v, want blocked", status)
		}
		assertLedgerDataForTypesRedacted(t, events, []string{RunEventError, RunEventBlocked},
			"RAW-PROMPT-TMUX-DO-NOT-LOG", "REDACTED_TEST_SECRET", "sk-tmuxsendsecret123")
		if eventsContainType(events, RunEventAdapterSend) {
			t.Fatalf("adapter_send recorded after failed send: %v", eventTypes(events))
		}
	})

	t.Run("capture failure", func(t *testing.T) {
		client := &fakeTmuxHarnessClient{
			sessions:             []string{"tmux-scout"},
			captureErrEchoPrompt: true,
		}
		status, events := runTmuxFormationForTest(t, client, board)
		if status.Status != RunStatusBlocked {
			t.Fatalf("status = %+v, want blocked", status)
		}
		assertLedgerDataForTypesRedacted(t, events, []string{RunEventAdapterSend, RunEventError, RunEventBlocked},
			"RAW-PROMPT-TMUX-DO-NOT-LOG", "REDACTED_TEST_SECRET", "sk-tmuxcapturesecret123")
		adapterSend := eventOfType(t, events, RunEventAdapterSend)
		if adapterSend.Data["promptSha256"] == "" || adapterSend.Data["sent"] != true {
			t.Fatalf("adapter_send data = %#v, want hash-only sent record", adapterSend.Data)
		}
	})
}

func TestTmuxAdapterHappyPathRecordsSendCompletionAndOutput(t *testing.T) {
	client := &fakeTmuxHarnessClient{
		sessions: []string{"tmux-scout"},
		artifact: "reports/tmux-happy.md",
	}
	status, events := runTmuxFormationForTest(t, client, "")
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	for _, eventType := range []string{RunEventSlotDispatch, RunEventAdapterSend, RunEventSlotResult, RunEventNodeOutput} {
		if !eventsContainType(events, eventType) {
			t.Fatalf("events = %v, want %s", eventTypes(events), eventType)
		}
	}
	if client.sendCalls != 1 || client.captureCalls != 2 {
		t.Fatalf("fake client calls send=%d capture=%d, want one send and two fake captures (readiness + native result)", client.sendCalls, client.captureCalls)
	}
	if len(client.created) != 1 {
		t.Fatalf("created sessions = %v, want exactly one on-demand owned session", client.created)
	}
	ownedSession := client.created[0]
	adapterSend := eventOfType(t, events, RunEventAdapterSend)
	if adapterSend.Data["adapter"] != "tmux" || adapterSend.Data["sessionRef"] != "tmux:"+ownedSession || adapterSend.Data["promptSha256"] == "" {
		t.Fatalf("adapter_send data = %#v, want owned tmux session %q and prompt hash", adapterSend.Data, ownedSession)
	}
	if client.sendTargets[0] != ownedSession {
		t.Fatalf("send target = %q, want the owned session %q, never the foreign fixture", client.sendTargets[0], ownedSession)
	}
	if got, want := client.killed, []string{ownedSession}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("killed sessions = %v, want the one owned session torn down %v", got, want)
	}
	slotResult := eventOfType(t, events, RunEventSlotResult)
	sentinel, ok := slotResult.Data["sentinel"].(map[string]any)
	if !ok {
		t.Fatalf("slot_result sentinel = %#v, want object", slotResult.Data["sentinel"])
	}
	if sentinel["artifact"] != "reports/tmux-happy.md" || slotResult.Data["status"] != "ok" {
		t.Fatalf("slot_result data = %#v, want parsed completion sentinel", slotResult.Data)
	}
	output := eventOfType(t, events, RunEventNodeOutput)
	if !strings.Contains(fmt.Sprint(output.Data["text"]), "agent output") {
		t.Fatalf("node_output = %#v, want captured agent output text", output.Data)
	}
}

func TestTmuxExecutorOnlyCreatesAndKillsOwnedSessionsOnSharedSocket(t *testing.T) {
	// The shared cockpit socket also hosts the operator's interactive terminals
	// and other live agent sessions. Seed those as foreign fixtures and prove the
	// executor spawns exactly one uniquely-named session, only ever operates on
	// that session, tears down only that session, never issues kill-server, and
	// never disrupts a foreign session.
	foreign := []string{"sol", "terminal-1", "terminal-2", "mission-real-agent-smoke-0644", "0"}
	client := &fakeTmuxHarnessClient{
		sessions: append([]string(nil), foreign...),
		artifact: "reports/tmux-shared.md",
	}
	status, _ := runTmuxFormationForTest(t, client, "")
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}

	if len(client.created) != 1 {
		t.Fatalf("created sessions = %v, want exactly one owned on-demand session", client.created)
	}
	owned := client.created[0]
	if !safeTmuxSessionName(owned) {
		t.Fatalf("owned session name %q is not a safe tmux target", owned)
	}
	foreignSet := map[string]bool{}
	for _, name := range foreign {
		foreignSet[name] = true
	}
	if foreignSet[owned] {
		t.Fatalf("owned session name %q collides with a foreign session", owned)
	}

	// Every recorded tmux op either enumerates sessions (no target) or targets the
	// one owned session. There is structurally no kill-server, and no op may ever
	// name a foreign session.
	for _, op := range client.ops {
		if op.op == "kill-server" {
			t.Fatalf("executor issued kill-server on the shared socket")
		}
		if op.target == "" {
			continue
		}
		if op.target != owned {
			t.Fatalf("tmux op %q targeted %q; the executor must only ever touch the session it created (%q)", op.op, op.target, owned)
		}
	}
	if got, want := client.killed, []string{owned}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("killed sessions = %v, want only the owned session torn down %v", got, want)
	}

	// Every foreign session is still present and untouched after the run.
	live := map[string]bool{}
	for _, name := range client.liveSessions() {
		live[name] = true
	}
	for _, name := range foreign {
		if !live[name] {
			t.Fatalf("foreign session %q was disrupted; it must remain live and untouched", name)
		}
	}
}

func TestTmuxExecutorLazyStartsServerOnEmptySocket(t *testing.T) {
	// Owner ruling: Formations supports ANY configured tmux socket, including one
	// with no pre-existing server. Model that by removing the socket fixture and
	// reporting no live server: the executor must lazy-start a keeper (which, like
	// tmux, materializes the socket), then create/own/tear-down its run session
	// exactly as on an existing server — never kill-server, never tearing down the
	// keeper.
	cfg := tmuxTestConfig(t)
	if err := os.Remove(cfg.Socket); err != nil {
		t.Fatalf("remove socket fixture to model empty socket: %v", err)
	}
	client := &fakeTmuxHarnessClient{noServer: true, artifact: "reports/tmux-lazy.md"}
	status, _ := runTmuxFormationForTestWithConfig(t, client, "", cfg)
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}

	keeper := cfg.SessionPrefix + "keeper"
	if got, want := client.keepersStarted, []string{keeper}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("keepers started = %v, want exactly one lazy-start keeper %v", got, want)
	}
	if !safeTmuxSessionName(keeper) {
		t.Fatalf("keeper name %q is not a safe tmux target", keeper)
	}
	if len(client.created) != 1 {
		t.Fatalf("created sessions = %v, want exactly one owned run session", client.created)
	}
	run := client.created[0]
	if run == keeper {
		t.Fatalf("owned run session %q must be distinct from the keeper", run)
	}

	for _, op := range client.ops {
		if op.op == "kill-server" {
			t.Fatalf("executor issued kill-server on the lazy-started socket")
		}
	}
	// Only the owned run session is torn down; the keeper is infrastructure and is
	// never reclaimed by teardown.
	if got, want := client.killed, []string{run}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("killed sessions = %v, want only the owned run session %v (keeper must never be torn down)", got, want)
	}
	live := map[string]bool{}
	for _, name := range client.liveSessions() {
		live[name] = true
	}
	if !live[keeper] {
		t.Fatalf("keeper %q must remain live after the run; it is left running by design", keeper)
	}
}

func TestTmuxExecutorReusesExistingServer(t *testing.T) {
	// When a server is already running on the configured socket, the executor must
	// use it as-is: no second keeper is lazy-started and the existing server is
	// left untouched. Seed foreign sessions to model a live shared server.
	foreign := []string{"sol", "terminal-1"}
	client := &fakeTmuxHarnessClient{
		sessions: append([]string(nil), foreign...),
		artifact: "reports/tmux-existing.md",
	}
	status, _ := runTmuxFormationForTest(t, client, "")
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}

	if len(client.keepersStarted) != 0 {
		t.Fatalf("keepers started = %v, want none when a server already exists", client.keepersStarted)
	}
	for _, op := range client.ops {
		if op.op == "start-keeper" {
			t.Fatalf("executor lazy-started a keeper despite an existing server")
		}
		if op.op == "kill-server" {
			t.Fatalf("executor issued kill-server on the existing server")
		}
	}
	if len(client.created) != 1 {
		t.Fatalf("created sessions = %v, want exactly one owned run session", client.created)
	}
	if got, want := client.killed, []string{client.created[0]}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("killed sessions = %v, want only the owned run session %v", got, want)
	}
	// Foreign sessions on the existing server remain live and untouched.
	live := map[string]bool{}
	for _, name := range client.liveSessions() {
		live[name] = true
	}
	for _, name := range foreign {
		if !live[name] {
			t.Fatalf("foreign session %q was disrupted; the existing server must be left untouched", name)
		}
	}
}

func TestPickOwnedSessionNameFailsClosedOnForeignCollision(t *testing.T) {
	cfg := tmuxTestConfig(t)
	cfg.Mission = "proof"
	client := &fakeTmuxHarnessClient{sessions: []string{"form-proof-run_x-slot_y"}}
	e := newTmuxFormationExecutorWithClient(nil, nil, cfg, client)
	if err := e.validateConfiguredBoundary(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pickOwnedSessionName(context.Background(), "run_x", "slot_y"); err == nil {
		t.Fatal("collision accepted")
	}
	if len(client.created) != 0 || len(client.killed) != 0 || client.sendCalls != 0 {
		t.Fatal("foreign session touched")
	}
}

func TestTmuxExecutorParsesNamedOutputPayloadBlockForPortRouting(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4NamedOutputBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	cfg := tmuxTestConfig(t)
	client := &fakeTmuxHarnessClient{
		sessions: []string{"tmux-scout"},
		pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
		captures: []string{
			"splitter produced two routed outputs\n```chrote-outputs\n{\"port_split_left\":{\"text\":\"LEFT-FROM-TMUX\"},\"port_split_right\":{\"text\":\"RIGHT-FROM-TMUX\"}}\n```\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=split.md>>>",
			"left consumer saw LEFT-FROM-TMUX\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=left.md>>>",
			"right consumer saw RIGHT-FROM-TMUX\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=right.md>>>",
		},
	}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
		Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	if len(client.sentPrompts) == 0 || !strings.Contains(client.sentPrompts[0], "```chrote-outputs") {
		t.Fatalf("split prompt missing chrote-outputs contract:\n%s", client.sentPrompts[0])
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	splitOutput := findNodeOutputEvent(t, events, "fmn_split")
	outputs, ok := splitOutput.Data["outputs"].(map[string]any)
	if !ok {
		t.Fatalf("split output payloads = %#v, want map", splitOutput.Data["outputs"])
	}
	assertOutputPayloadText(t, outputs, "port_split_left", "LEFT-FROM-TMUX")
	assertOutputPayloadText(t, outputs, "port_split_right", "RIGHT-FROM-TMUX")
	if got, want := firstStartedInputText(t, events, "fmn_left"), "LEFT-FROM-TMUX"; got != want {
		t.Fatalf("left routed input = %q, want %q", got, want)
	}
	if got, want := firstStartedInputText(t, events, "fmn_right"), "RIGHT-FROM-TMUX"; got != want {
		t.Fatalf("right routed input = %q, want %q", got, want)
	}
}

func TestTmuxExecutorReadsOutputRefArtifactForPortRouting(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4NamedOutputBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	leftArtifact := filepath.Join(store.Workspace, ".formations", "artifacts", "left-long.md")
	leftArtifactRef, err := filepath.Rel(store.Workspace, leftArtifact)
	if err != nil {
		t.Fatalf("relative artifact ref: %v", err)
	}
	leftArtifactRef = filepath.ToSlash(leftArtifactRef)
	longLeft := "LEFT-ARTIFACT-BEGIN\n" + strings.Repeat("long routed artifact line with preserved spacing 0123456789\n", 80) + "LEFT-ARTIFACT-END"
	writeFixture(t, leftArtifact, longLeft)
	payloads := map[string]FormationOutputPayload{
		"port_split_left":  {Text: "LEFT-SUMMARY", Ref: leftArtifactRef},
		"port_split_right": {Text: "RIGHT-FROM-TMUX"},
	}
	rawPayloads, err := json.Marshal(payloads)
	if err != nil {
		t.Fatalf("marshal output payloads: %v", err)
	}
	cfg := tmuxTestConfig(t)
	cfg.Cwd = store.Workspace
	cfg.Roots = []string{store.Workspace}
	client := &fakeTmuxHarnessClient{
		sessions: []string{"tmux-scout"},
		pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
		captures: []string{
			"splitter wrote long payload artifact\n```chrote-outputs\n" + string(rawPayloads) + "\n```\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=split.md>>>",
			"left consumer saw artifact payload\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=left.md>>>",
			"right consumer saw RIGHT-FROM-TMUX\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=right.md>>>",
		},
	}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
		Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	splitOutput := findNodeOutputEvent(t, events, "fmn_split")
	outputs, ok := splitOutput.Data["outputs"].(map[string]any)
	if !ok {
		t.Fatalf("split output payloads = %#v, want map", splitOutput.Data["outputs"])
	}
	assertOutputPayloadText(t, outputs, "port_split_left", longLeft)
	assertOutputPayloadText(t, outputs, "port_split_right", "RIGHT-FROM-TMUX")
	leftPayload, ok := outputs["port_split_left"].(map[string]any)
	if !ok || leftPayload["ref"] != leftArtifactRef {
		t.Fatalf("left payload = %#v, want ref %q", outputs["port_split_left"], leftArtifactRef)
	}
	if got := firstStartedInputText(t, events, "fmn_left"); got != longLeft {
		t.Fatalf("left routed input length=%d, want artifact length=%d", len(got), len(longLeft))
	}
	if len(client.sentPrompts) < 2 || !strings.Contains(client.sentPrompts[1], longLeft) {
		t.Fatalf("left consumer prompt did not receive hydrated artifact body; prompts=%d", len(client.sentPrompts))
	}
}

func TestTmuxExecutorBlocksInvalidOutputRefArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setupRef func(t *testing.T, store *Store) string
		wantCode string
	}{
		{
			name: "missing_ref_file",
			setupRef: func(t *testing.T, store *Store) string {
				t.Helper()
				return filepath.Join(store.Workspace, ".formations", "artifacts", "missing-left.md")
			},
			wantCode: "unavailable_output_ref",
		},
		{
			name: "non_regular_ref_directory",
			setupRef: func(t *testing.T, store *Store) string {
				t.Helper()
				dir := filepath.Join(store.Workspace, ".formations", "artifacts", "directory-ref")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("create directory ref fixture: %v", err)
				}
				return dir
			},
			wantCode: "invalid_output_ref",
		},
		{
			name: "outside_configured_roots",
			setupRef: func(t *testing.T, store *Store) string {
				t.Helper()
				outside := filepath.Join(t.TempDir(), "outside-left.md")
				writeFixture(t, outside, "SHOULD-NOT-ROUTE")
				return outside
			},
			wantCode: "output_ref_outside_root",
		},
		{
			name: "symlink_escape",
			setupRef: func(t *testing.T, store *Store) string {
				t.Helper()
				outside := filepath.Join(t.TempDir(), "outside-left.md")
				writeFixture(t, outside, "SHOULD-NOT-ROUTE")
				insideLink := filepath.Join(store.Workspace, ".formations", "artifacts", "linked-outside.md")
				if err := os.MkdirAll(filepath.Dir(insideLink), 0o755); err != nil {
					t.Fatalf("mkdir symlink parent: %v", err)
				}
				if err := os.Symlink(outside, insideLink); err != nil {
					t.Fatalf("create symlink escape fixture: %v", err)
				}
				return insideLink
			},
			wantCode: "output_ref_outside_root",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			store.Now = fixedClock()
			personas.Now = fixedClock()
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4NamedOutputBoardFixture())
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatalf("read board: %v", err)
			}
			badRef := tc.setupRef(t, store)
			payloads := map[string]FormationOutputPayload{
				"port_split_left":  {Text: "LEFT-SUMMARY", Ref: badRef},
				"port_split_right": {Text: "RIGHT-FROM-TMUX"},
			}
			rawPayloads, err := json.Marshal(payloads)
			if err != nil {
				t.Fatalf("marshal output payloads: %v", err)
			}
			cfg := tmuxTestConfig(t)
			cfg.Cwd = store.Workspace
			cfg.Roots = []string{store.Workspace}
			client := &fakeTmuxHarnessClient{
				sessions: []string{"tmux-scout"},
				pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
				captures: []string{
					"splitter referenced invalid artifact\n```chrote-outputs\n" + string(rawPayloads) + "\n```\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=split.md>>>",
				},
			}
			executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			engine := NewRunEngine(store, personas, executor)
			status, err := engine.RunMission("session-search", RunStartRequest{
				MissionID:         "mis_showcase",
				Actor:             "agent:test",
				ExpectedBoardETag: board.ETag,
				ExpectedBoardRev:  board.Rev,
				Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 1},
			})
			if err != nil {
				t.Fatalf("run mission: %v", err)
			}
			if status.Status != RunStatusBlocked || status.Final {
				t.Fatalf("status = %+v, want blocked non-final", status)
			}
			events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
			errEvent := eventOfType(t, events, RunEventError)
			if errEvent.NodeID != "fmn_split" || errEvent.Data["code"] != tc.wantCode || errEvent.Data["boundary"] != "executor" {
				t.Fatalf("error event = %+v data=%#v, want %s on fmn_split/executor", errEvent, errEvent.Data, tc.wantCode)
			}
			if eventsContainNodeStart(events, "fmn_left") {
				t.Fatalf("events = %v, invalid ref must not route to fmn_left", eventTypesOf(events))
			}
		})
	}
}

func eventsContainNodeStart(events []RunEvent, nodeID string) bool {
	for _, event := range events {
		if event.Type == RunEventNodeStarted && event.NodeID == nodeID {
			return true
		}
	}
	return false
}

func TestTmuxPeerFormationUsesSharedPlaneAndFacilitatorSynthesis(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	for _, id := range []string{"peer-a", "peer-b"} {
		createS4Persona(t, personas, id)
	}
	writeFixture(t, store.BoardPath("session-search"), tmuxPeerBoardFixture())
	cfg := tmuxTestConfig(t)
	client := &fakeTmuxHarnessClient{
		sessions: []string{"tmux-peer-a", "tmux-peer-b"},
		pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
		captures: []string{
			"PEER-A-BOUNDARY: first peer contribution about runtime boundary\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=peer-a.md>>>",
			"PEER-B-EVIDENCE: second peer read PEER-A-BOUNDARY and added evidence\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=peer-b.md>>>",
			"PEER-COLLABORATED: synthesis uses PEER-A-BOUNDARY and PEER-B-EVIDENCE from the shared plane\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=peer-final.md>>>",
		},
	}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunFormation("session-search", "fmn_peer", FormationRunRequest{
		Actor:  "agent:test",
		Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("run peer formation: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	if len(client.created) != 2 {
		t.Fatalf("created sessions = %v, want one owned session per peer", client.created)
	}
	ownedA, ownedB := client.created[0], client.created[1]
	if got, want := client.sendTargets, []string{ownedA, ownedB, ownedA}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("send targets = %v, want peer-turn/turn/facilitator on owned sessions %v (facilitator reuses peer A)", got, want)
	}
	for _, foreign := range []string{"tmux-peer-a", "tmux-peer-b"} {
		for _, target := range client.sendTargets {
			if target == foreign {
				t.Fatalf("send targeted foreign session %q; executor must only dispatch to sessions it created", foreign)
			}
		}
	}
	if len(client.sentPrompts) != 3 {
		t.Fatalf("sent prompts = %d, want 3 peer prompts", len(client.sentPrompts))
	}
	for i, prompt := range client.sentPrompts {
		for _, want := range []string{"shared peer plane:", "Read the shared peer plane before deciding your next move", "tmux -S " + cfg.Socket + " capture-pane"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt %d missing %q:\n%s", i, want, prompt)
			}
		}
	}
	if !strings.Contains(client.sentPrompts[2], "peer phase: facilitator") || !strings.Contains(client.sentPrompts[2], "temporary facilitator") {
		t.Fatalf("facilitator prompt missing facilitator instructions:\n%s", client.sentPrompts[2])
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	planeEvent := eventOfType(t, events, "peer_plane")
	planeRel, ok := planeEvent.Data["path"].(string)
	if !ok || planeRel == "" {
		t.Fatalf("peer_plane event data = %#v, want path", planeEvent.Data)
	}
	planeRaw, err := os.ReadFile(filepath.Join(store.Workspace, filepath.FromSlash(planeRel)))
	if err != nil {
		t.Fatalf("read shared peer plane %q: %v", planeRel, err)
	}
	plane := string(planeRaw)
	for _, want := range []string{"# Peer Plane", "peer-a", "peer-b", "PEER-A-BOUNDARY", "PEER-B-EVIDENCE", "PEER-COLLABORATED"} {
		if !strings.Contains(plane, want) {
			t.Fatalf("shared peer plane missing %q:\n%s", want, plane)
		}
	}
	var phases []string
	for _, event := range events {
		if event.Type == RunEventSlotDispatch {
			phases = append(phases, fmt.Sprint(event.Data["phase"]))
		}
	}
	if got, want := phases, []string{"peer-turn", "peer-turn", "peer-facilitator"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("dispatch phases = %v, want %v", got, want)
	}
	output := eventOfType(t, events, RunEventNodeOutput)
	text := fmt.Sprint(output.Data["text"])
	if !strings.Contains(text, "PEER-COLLABORATED") || !strings.Contains(text, "PEER-A-BOUNDARY") || !strings.Contains(text, "PEER-B-EVIDENCE") {
		t.Fatalf("node_output = %#v, want peer synthesis grounded in both contributions", output.Data)
	}
	if strings.Contains(text, "tmux harness completed") {
		t.Fatalf("node_output = %#v, want synthesis not independent harness summaries", output.Data)
	}
}

func TestPeerPlaneAppendStaysOnPinnedRunDirectoryAfterPathSwap(t *testing.T) {
	executor, req, runDirectory := peerPlaneSecurityFixture(t)
	plane, err := executor.seedPeerPlane(req, nil)
	if err != nil {
		t.Fatalf("seed peer plane: %v", err)
	}
	defer plane.close()
	if got, want := plane.relativePath, runArtifactPath("session-search", req.RunID, ".peer.md"); got != want {
		t.Fatalf("peer plane relative path = %q, want %q", got, want)
	}
	detachedDirectory := runDirectory + ".detached"
	if err := os.Rename(runDirectory, detachedDirectory); err != nil {
		t.Fatalf("detach pinned run directory: %v", err)
	}
	if err := os.MkdirAll(runDirectory, 0o770); err != nil {
		t.Fatalf("create replacement run directory: %v", err)
	}
	replacementPlane := filepath.Join(runDirectory, req.RunID+".peer.md")
	const replacementBefore = "replacement directory must not receive peer output\n"
	writeFixture(t, replacementPlane, replacementBefore)

	if err := executor.appendPeerPlaneOutput(plane, tmuxSlotOutput{
		SlotID: "slot_peer", AgentID: "peer-a", Phase: "peer-turn", Text: "PINNED-PEER-CONTRIBUTION",
	}); err != nil {
		t.Fatalf("append through pinned run directory: %v", err)
	}
	if got := readFile(t, replacementPlane); got != replacementBefore {
		t.Fatalf("replacement run directory was mutated: %q", got)
	}
	detachedPlane := filepath.Join(detachedDirectory, req.RunID+".peer.md")
	if got := readFile(t, detachedPlane); !strings.Contains(got, "PINNED-PEER-CONTRIBUTION") {
		t.Fatalf("pinned peer plane missing contribution:\n%s", got)
	}
}

func TestPeerPlaneAppendRejectsSymlinkAndHardlinkSubstitution(t *testing.T) {
	tests := []struct {
		name       string
		substitute func(t *testing.T, victimPath, planePath string)
	}{
		{
			name: "symlink",
			substitute: func(t *testing.T, victimPath, planePath string) {
				t.Helper()
				if err := os.Symlink(victimPath, planePath); err != nil {
					t.Fatalf("substitute peer plane symlink: %v", err)
				}
			},
		},
		{
			name: "hardlink",
			substitute: func(t *testing.T, victimPath, planePath string) {
				t.Helper()
				if err := os.Link(victimPath, planePath); err != nil {
					t.Fatalf("substitute peer plane hardlink: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, req, runDirectory := peerPlaneSecurityFixture(t)
			plane, err := executor.seedPeerPlane(req, nil)
			if err != nil {
				t.Fatalf("seed peer plane: %v", err)
			}
			defer plane.close()
			planePath := filepath.Join(runDirectory, req.RunID+".peer.md")
			if err := os.Remove(planePath); err != nil {
				t.Fatalf("remove seeded peer plane: %v", err)
			}
			victimPath := filepath.Join(t.TempDir(), "peer-plane-victim")
			const victimBefore = "private victim content\n"
			writeFixture(t, victimPath, victimBefore)
			test.substitute(t, victimPath, planePath)

			err = executor.appendPeerPlaneOutput(plane, tmuxSlotOutput{
				SlotID: "slot_peer", AgentID: "peer-a", Phase: "peer-turn", Text: "must be rejected",
			})
			if !errors.Is(err, ErrRunLedgerInvalid) {
				t.Fatalf("append through %s error = %v, want ErrRunLedgerInvalid", test.name, err)
			}
			if got := readFile(t, victimPath); got != victimBefore {
				t.Fatalf("rejected %s append mutated victim: %q", test.name, got)
			}
		})
	}
}

func TestPeerPlaneAppendRejectsAggregateByteOverflowWithoutMutation(t *testing.T) {
	executor, req, runDirectory := peerPlaneSecurityFixture(t)
	plane, err := executor.seedPeerPlane(req, nil)
	if err != nil {
		t.Fatalf("seed peer plane: %v", err)
	}
	defer plane.close()
	planePath := filepath.Join(runDirectory, req.RunID+".peer.md")
	before := strings.Repeat("x", peerPlaneMaxBytes)
	writeFixture(t, planePath, before)

	err = executor.appendPeerPlaneOutput(plane, tmuxSlotOutput{
		SlotID: "slot_peer", AgentID: "peer-a", Phase: "peer-turn", Text: "one byte too many",
	})
	if !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("append beyond peer plane byte cap error = %v, want ErrRunLedgerInvalid", err)
	}
	if got := readFile(t, planePath); got != before {
		t.Fatalf("rejected oversized peer append mutated plane")
	}
}

func peerPlaneSecurityFixture(t *testing.T) (*TmuxFormationExecutor, FormationExecution, string) {
	t.Helper()
	workspace := t.TempDir()
	store := NewStore(workspace)
	runID := newPrefixedID("run")
	ledgerPath := filepath.Join(workspace, runArtifactPath("session-search", runID, ".ndjson"))
	writeFixture(t, ledgerPath, string(testRunLedgerBytes(t, testRunStartedEvent(runID, "session-search"))))
	executor := newTmuxFormationExecutorWithClient(store, nil, tmuxTestConfig(t), &fakeTmuxHarnessClient{})
	return executor, FormationExecution{
		RunID: runID, NodeID: "fmn_peer", Brief: FormationBrief{Goal: "Coordinate safely"},
	}, filepath.Dir(ledgerPath)
}

func TestTmuxOrchestratedFormationGivesLeaderToolPacketWithoutPreDispatchingWorkers(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	for _, id := range []string{"lead", "worker-a", "worker-b"} {
		createS4Persona(t, personas, id)
	}
	writeFixture(t, store.BoardPath("session-search"), tmuxOrchestratedBoardFixture())
	cfg := tmuxTestConfig(t)
	client := &fakeTmuxHarnessClient{
		sessions: []string{"tmux-lead", "tmux-worker-a", "tmux-worker-b"},
		pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
		captures: []string{
			"FINAL-SYNTHESIS: leader used tmux to prompt worker-a and inspect worker-b before finishing\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=final.md>>>",
		},
	}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunFormation("session-search", "fmn_orch", FormationRunRequest{
		Actor:  "agent:test",
		Limits: RunLimits{MaxDispatch: 6, MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("run orchestrated formation: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded final", status)
	}
	// created order follows binding resolution: controller, worker A, worker B.
	if len(client.created) != 3 {
		t.Fatalf("created sessions = %v, want one owned session for the leader and each worker", client.created)
	}
	ownedLead, ownedWorkerA := client.created[0], client.created[1]
	if got, want := client.sendTargets, []string{ownedLead}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("send targets = %v, want only the leader's owned session %v", got, want)
	}
	for _, foreign := range []string{"tmux-lead", "tmux-worker-a", "tmux-worker-b"} {
		if client.sendTargets[0] == foreign {
			t.Fatalf("leader dispatch targeted foreign session %q; executor must only dispatch to sessions it created", foreign)
		}
	}
	if len(client.sentPrompts) != 1 {
		t.Fatalf("sent prompts = %d, want 1 leader prompt", len(client.sentPrompts))
	}
	leaderPrompt := client.sentPrompts[0]
	for _, want := range []string{
		"orchestration phase: leader-agentic",
		"formation team packet:",
		"tmux socket: " + cfg.Socket,
		"- slot slot_worker_a label=\"Worker A\" agent=\"worker-a\" harness=\"openai-codex\" session=\"" + ownedWorkerA + "\"",
		"tmux -S " + cfg.Socket + " capture-pane -t " + ownedWorkerA + " -p -S -120",
		"paste ONLY this exact pointer",
		"Use native tmux/shell tools to steer the worker sessions yourself",
	} {
		if !strings.Contains(leaderPrompt, want) {
			t.Fatalf("leader prompt missing %q:\n%s", want, leaderPrompt)
		}
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	var phases []string
	var teamEvents int
	for _, event := range events {
		if event.Type == RunEventSlotDispatch {
			phases = append(phases, fmt.Sprint(event.Data["phase"]))
		}
		if event.Type == RunEventOrchestrationTeam {
			teamEvents++
		}
	}
	if got, want := phases, []string{"leader-agentic"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("dispatch phases = %v, want %v", got, want)
	}
	if teamEvents != 1 {
		t.Fatalf("orchestration team events = %d, want 1", teamEvents)
	}
	output := eventOfType(t, events, RunEventNodeOutput)
	if !strings.Contains(fmt.Sprint(output.Data["text"]), "FINAL-SYNTHESIS") {
		t.Fatalf("node_output = %#v, want leader final output", output.Data)
	}
}

// TestTmuxOrchestratedFormationRejectsInvalidSlotShapeBeforeDispatch covers the
// slot-shape errors splitOrchestratedSlots raises before the executor resolves
// any slot binding or touches tmux: no controller slot, a controller with no
// worker slots, and (optionally) more than one controller slot. Each case must
// fail fast with the matching runExecutionError code, visible in the run
// ledger, and without a single tmux call.
func TestTmuxOrchestratedFormationRejectsInvalidSlotShapeBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		board    string
		agentIDs []string
		wantCode string
	}{
		{
			name:     "no controller slot",
			board:    tmuxOrchestratedBoardFixtureNoController(),
			agentIDs: []string{"worker-a", "worker-b"},
			wantCode: "missing_controller",
		},
		{
			name:     "controller with no worker slots",
			board:    tmuxOrchestratedBoardFixtureNoWorkers(),
			agentIDs: []string{"lead"},
			wantCode: "missing_worker",
		},
		{
			name:     "two controller slots",
			board:    tmuxOrchestratedBoardFixtureTwoControllers(),
			agentIDs: []string{"lead", "co-lead", "worker-a"},
			wantCode: "ambiguous_controller",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			store.Now = fixedClock()
			personas.Now = fixedClock()
			for _, id := range tc.agentIDs {
				createS4Persona(t, personas, id)
			}
			writeFixture(t, store.BoardPath("session-search"), tc.board)
			cfg := tmuxTestConfig(t)
			client := &fakeTmuxHarnessClient{}
			executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			engine := NewRunEngine(store, personas, executor)
			status, err := engine.RunFormation("session-search", "fmn_orch", FormationRunRequest{
				Actor:  "agent:test",
				Limits: RunLimits{MaxDispatch: 6, MaxAttempts: 1},
			})
			if err != nil {
				t.Fatalf("run orchestrated formation: %v", err)
			}
			if status.Status != RunStatusBlocked {
				t.Fatalf("status = %+v, want blocked", status)
			}
			events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
			errEvent := eventOfType(t, events, RunEventError)
			if errEvent.Data["code"] != tc.wantCode {
				t.Fatalf("run error code = %#v, want %s", errEvent.Data["code"], tc.wantCode)
			}
			if errEvent.NodeID != "fmn_orch" {
				t.Fatalf("error nodeId = %q, want fmn_orch", errEvent.NodeID)
			}
			if eventsContainType(events, RunEventOrchestrationTeam) || eventsContainType(events, RunEventSlotDispatch) {
				t.Fatalf("events after slot-shape rejection = %v, want no orchestration_team/slot_dispatch", eventTypes(events))
			}
			if len(client.created) != 0 || client.listCalls != 0 || client.describeCalls != 0 || client.captureCalls != 0 || client.sendCalls != 0 {
				t.Fatalf("tmux client calls after slot-shape rejection: created=%v list=%d describe=%d capture=%d send=%d, want none",
					client.created, client.listCalls, client.describeCalls, client.captureCalls, client.sendCalls)
			}
		})
	}
}

// TestTmuxOrchestratedFormationRecordsPerWorkerCaptureEvidence pins the
// observability half of ADR-0011: every bound worker gets an outcome event
// derived from Archon's OWN pane observation — a bind-time baseline capture
// diffed against a completion capture — never from the leader's self-report and
// never by Archon prompting the worker. Worker A is touched by the leader and
// its produced text must be inspectable in the ledger; worker B is left
// untouched and must surface as a distinct anomaly rather than as silence.
func TestWorkerObservationExcerptKeepsNewPaneTextWithinCap(t *testing.T) {
	grew, truncated := workerObservationExcerpt("worker idle", "worker idle\nWORKER-RESULT: done", 4096)
	if grew != "WORKER-RESULT: done" || truncated {
		t.Fatalf("grown pane excerpt = %q truncated=%v, want only the text after the baseline", grew, truncated)
	}

	scrolled, truncated := workerObservationExcerpt("worker idle", "later output only", 4096)
	if scrolled != "later output only" || truncated {
		t.Fatalf("scrolled pane excerpt = %q truncated=%v, want the whole capture", scrolled, truncated)
	}

	long := strings.Repeat("ä", 200)
	capped, truncated := workerObservationExcerpt("", long, 64)
	if !truncated {
		t.Fatalf("oversized pane excerpt truncated=%v, want true", truncated)
	}
	if len(capped) > 64 {
		t.Fatalf("oversized pane excerpt = %d bytes, want at most the 64 byte cap", len(capped))
	}
	if !utf8.ValidString(capped) {
		t.Fatalf("oversized pane excerpt %q is not valid UTF-8", capped)
	}
	if !strings.HasSuffix(long, capped) {
		t.Fatalf("oversized pane excerpt = %q, want the tail of the capture", capped)
	}

	secret, _ := workerObservationExcerpt("", "worker ran with token=REDACTED_TEST_SECRET", 4096)
	if strings.Contains(secret, "REDACTED_TEST_SECRET") {
		t.Fatalf("pane excerpt %q leaked a secret-shaped token past ledger redaction", secret)
	}
}

// workerObservationsBySlot indexes the per-worker observation events by slot and
// fails on a duplicate, since each bound worker must produce exactly one outcome
// event per run.
func workerObservationsBySlot(t *testing.T, events []RunEvent) map[string]RunEvent {
	t.Helper()
	observations := map[string]RunEvent{}
	for _, event := range events {
		if event.Type != RunEventWorkerObservation {
			continue
		}
		slotID := fmt.Sprint(event.Data["slotId"])
		if _, ok := observations[slotID]; ok {
			t.Fatalf("duplicate worker_observation for slot %s", slotID)
		}
		observations[slotID] = event
	}
	return observations
}

func TestExtractCapturedSlotTextRemovesWrappedPromptEcho(t *testing.T) {
	captured := strings.Join([]string{
		"run: run_test",
		"node: fmn_orch",
		"slot: slot_lead",
		"orchestration phase: controller-synthesis",
		"brief: hidden prompt body",
		"When complete, emit exactly one sentinel line using the run value above: <<<CHROTE-DONE run-id=<the-run-value-above> status=ok artifact=<pat",
		"h-or-ref>>>",
		"FINAL-SYNTHESIS: safe final answer",
		"<<<CHROTE-DONE run-id=run_test status=ok artifact=artifacts/final.md>>>",
	}, "\n")

	text := extractCapturedSlotText(captured, "", "run_test")
	if text != "FINAL-SYNTHESIS: safe final answer" {
		t.Fatalf("extracted slot text = %q, want only final answer without prompt echo", text)
	}
}

func TestExtractCapturedSlotTextKeepsFinalTurnAfterTranscriptNoise(t *testing.T) {
	captured := strings.Join([]string{
		"controller plan:",
		"• Worker A: identify boundary",
		"When complete, emit exactly one sentinel line using the run value above: <<<CHROTE-DONE run-id=<the-run-value-above> status=ok artifact=<path-or-ref>>>",
		"worker outputs:",
		"• API/runtime boundary: Archon/Formations to tmux Codex.",
		"────────────────────────────────────────────────────────────────────────────────────────────────",
		"",
		"• Runtime-mediated orchestrated control works at the smoke level.",
		"  Worker dispatch and final synthesis returned through Codex sentinels.",
		"<<<CHROTE-DONE run-id=run_final status=ok artifact=final-synthesis>>>",
	}, "\n")

	text := extractCapturedSlotText(captured, "", "run_final")
	want := "• Runtime-mediated orchestrated control works at the smoke level.\n  Worker dispatch and final synthesis returned through Codex sentinels."
	if text != want {
		t.Fatalf("extracted slot text = %q, want %q", text, want)
	}
}

func TestTmuxRenderedPromptDoesNotContainParseableActualRunSentinel(t *testing.T) {
	runID := "run_prompt_echo_regression"
	executor := &TmuxFormationExecutor{config: TmuxExecutorConfig{Cwd: "/tmp/chrote-test"}}
	prompt := executor.renderPrompt(FormationExecution{
		RunID:  runID,
		NodeID: "fmn_research",
		Brief:  FormationBrief{Goal: "verify prompt echo cannot complete dispatch"},
		Inputs: []RunInputRef{{Text: "context"}},
	}, FormationSlot{ID: "slot_research"}, PersonaCard{ID: "scout"}, HarnessVariant{ID: "openai-codex"})

	if !strings.Contains(prompt, "run: "+runID+"\n") {
		t.Fatalf("rendered prompt = %q, want run line with actual run id", prompt)
	}
	if forbidden := "<<<CHROTE-DONE run-id=" + runID; strings.Contains(prompt, forbidden) {
		t.Fatalf("rendered prompt contains parseable actual-run sentinel prefix %q: %q", forbidden, prompt)
	}
	if sentinel, ok := ParseCompletionSentinel(prompt, runID); ok {
		t.Fatalf("prompt echo parsed as completion sentinel: %+v", sentinel)
	}

	actual := fmt.Sprintf("agent output\n<<<CHROTE-DONE run-id=%s status=ok artifact=reports/actual.md>>>\n", runID)
	sentinel, ok := ParseCompletionSentinel(actual, runID)
	if !ok || sentinel.RunID != runID || sentinel.Status != "ok" || sentinel.Artifact != "reports/actual.md" {
		t.Fatalf("actual emitted sentinel parsed = %+v ok=%v, want matching completion", sentinel, ok)
	}
}

func TestTmuxPaneShowsAgentWorking(t *testing.T) {
	for _, c := range []struct {
		harness, captured string
		working           bool
	}{
		{"claude-code", "❯ prompt still sitting unsent in the input box", false},
		{"openai-codex", "• Working (8s • esc to interrupt)", true},
		{"claude-code", "✶ Puzzling… (4s · ↓ 60 tokens)", true},
		{"claude-code", "  ⎿  Running… (3s)", true},
		{"claude-code", "✻ Crunched for 14s", false},
		// Claude's spinner rule is Claude's: Codex prose never reads as working.
		{"openai-codex", "• Summarised the notes… (3s read)", false},
	} {
		if got := tmuxPaneShowsAgentWorking(c.harness, c.captured); got != c.working {
			t.Errorf("%s %q: working = %t, want %t", c.harness, c.captured, got, c.working)
		}
	}
}

func countFakeTmuxCommands(t *testing.T, logPath string) int {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read fake tmux log for command count: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "ARGS	") {
			count++
		}
	}
	return count
}

func runTmuxFormationForTest(t *testing.T, client *fakeTmuxHarnessClient, board string) (*RunStatusProjection, []RunEvent) {
	t.Helper()
	return runTmuxFormationForTestWithConfig(t, client, board, tmuxTestConfig(t))
}

func runTmuxFormationForTestWithConfig(t *testing.T, client *fakeTmuxHarnessClient, board string, cfg TmuxExecutorConfig) (*RunStatusProjection, []RunEvent) {
	t.Helper()
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	harness := strings.TrimSpace(client.harness)
	if harness == "" {
		harness = "openai-codex"
	}
	if _, err := personas.CreatePersona(CreatePersonaRequest{
		ID:           "scout",
		Kind:         "specialist",
		Capabilities: []string{"research"},
		Harness:      harness,
	}); err != nil {
		t.Fatalf("create persona scout: %v", err)
	}
	if board == "" {
		board = s4RunBoardFixture()
	}
	if harness != "openai-codex" {
		board = strings.Replace(board, `harness = "openai-codex"`, `harness = "`+harness+`"`, 1)
		cfg.Harnesses = []string{harness}
	}
	writeFixture(t, store.BoardPath("session-search"), board)
	if client.pane.CurrentPath == "" && !client.pane.Dead {
		client.pane.CurrentPath = cfg.Cwd
	}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunFormation("session-search", "fmn_research", FormationRunRequest{
		Actor:  "agent:test",
		Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("run tmux formation: %v", err)
	}
	return status, readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
}

func tmuxTestConfig(t *testing.T) TmuxExecutorConfig {
	t.Helper()
	root := t.TempDir()
	socket := filepath.Join(root, "tmux.sock")
	if err := os.WriteFile(socket, []byte("disposable tmux socket identity fixture"), 0o600); err != nil {
		t.Fatalf("write disposable tmux socket fixture: %v", err)
	}
	return TmuxExecutorConfig{
		Harnesses:      []string{"openai-codex"},
		Socket:         socket,
		Cwd:            root,
		Roots:          []string{root},
		SessionPrefix:  "tmux-",
		OutputCapBytes: defaultTmuxOutputCapBytes,
		TimeoutSeconds: 1,
	}
}

// nonTempWorkspace builds a production-style config whose dedicated socket and
// workspace cwd/roots live OUTSIDE /tmp, mirroring the /srv + /run production
// layout. The base directory is created under the package directory (which is
// never /tmp) and removed on cleanup.
func nonTempWorkspace(t *testing.T) TmuxExecutorConfig {
	t.Helper()
	packageRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get package root: %v", err)
	}
	base, err := os.MkdirTemp(packageRoot, "chrote-prod-")
	if err != nil {
		t.Fatalf("create non-temp workspace base: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove non-temp workspace base: %v", err)
		}
	})
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create non-temp workspace: %v", err)
	}
	socketDir := filepath.Join(base, "formations-tmux")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		t.Fatalf("create non-temp socket dir: %v", err)
	}
	socket := filepath.Join(socketDir, "default")
	if err := os.WriteFile(socket, []byte("dedicated formations tmux socket fixture"), 0o600); err != nil {
		t.Fatalf("write non-temp socket fixture: %v", err)
	}
	return TmuxExecutorConfig{
		Harnesses:      []string{"openai-codex"},
		Socket:         socket,
		Cwd:            workspace,
		Roots:          []string{workspace},
		SessionPrefix:  "tmux-",
		OutputCapBytes: defaultTmuxOutputCapBytes,
		TimeoutSeconds: 1,
	}
}

func clearExecutorEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CHROTE_FORMATIONS_LAB_HARNESSES",
		"CHROTE_FORMATIONS_LAB_CWD",
		"CHROTE_FORMATIONS_LAB_ROOTS",
		"CHROTE_FORMATIONS_TMUX_HARNESSES",
		"CHROTE_FORMATIONS_TMUX_SOCKET",
		"CHROTE_FORMATIONS_TMUX_CWD",
		"CHROTE_FORMATIONS_TMUX_ROOTS",
		"CHROTE_FORMATIONS_TMUX_SESSION_PREFIX",
		"CHROTE_FORMATIONS_TMUX_PROD_SMOKE",
		"CHROTE_FORMATIONS_TMUX_DEDICATED",
	} {
		t.Setenv(key, "")
	}
}

func tmuxRunBoardWithBrief(goal string) string {
	return strings.Replace(s4RunBoardFixture(), "title = \"Research\"\n\n[[formation.input]]", "title = \"Research\"\n\n[formation.brief]\ngoal = "+renderString(goal)+"\nbeadId = \"home-7kc4.7\"\n\n[[formation.input]]", 1)
}

func tmuxOrchestratedBoardFixture() string {
	return tmuxOrchestratedBoardFixtureWithSlots(`[[formation.slot]]
id = "slot_lead"
label = "Lead"
controller = true
agentId = "lead"
harness = "openai-codex"

[[formation.slot]]
id = "slot_worker_a"
label = "Worker A"
controller = false
agentId = "worker-a"
harness = "openai-codex"

[[formation.slot]]
id = "slot_worker_b"
label = "Worker B"
controller = false
agentId = "worker-b"
harness = "openai-codex"
`)
}

// tmuxOrchestratedBoardFixtureWithSlots renders the brd_orch/fmn_orch header
// with a caller-supplied slot table; every orchestrated-board fixture,
// including the canonical tmuxOrchestratedBoardFixture, goes through it so a
// header change cannot silently fork the board shape between tests.
func tmuxOrchestratedBoardFixtureWithSlots(slots string) string {
	return `schema = 1
id = "brd_orch"
slug = "session-search"
title = "Orchestrated Smoke"
rev = 1
updatedBy = "agent:test"
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_orch"
type = "orchestrated"
title = "Proposal crew"

[formation.brief]
goal = "Produce a tiny implementation proposal with API and test sections"
beadId = "home-9hjn"

[[formation.input]]
id = "port_orch_in"
label = "Input"

[[formation.output]]
id = "port_orch_out"
label = "Output"

` + slots
}

func tmuxOrchestratedBoardFixtureNoController() string {
	return tmuxOrchestratedBoardFixtureWithSlots(`[[formation.slot]]
id = "slot_worker_a"
label = "Worker A"
controller = false
agentId = "worker-a"
harness = "openai-codex"

[[formation.slot]]
id = "slot_worker_b"
label = "Worker B"
controller = false
agentId = "worker-b"
harness = "openai-codex"
`)
}

func tmuxOrchestratedBoardFixtureNoWorkers() string {
	return tmuxOrchestratedBoardFixtureWithSlots(`[[formation.slot]]
id = "slot_lead"
label = "Lead"
controller = true
agentId = "lead"
harness = "openai-codex"
`)
}

func tmuxOrchestratedBoardFixtureTwoControllers() string {
	return tmuxOrchestratedBoardFixtureWithSlots(`[[formation.slot]]
id = "slot_lead"
label = "Lead"
controller = true
agentId = "lead"
harness = "openai-codex"

[[formation.slot]]
id = "slot_co_lead"
label = "Co-Lead"
controller = true
agentId = "co-lead"
harness = "openai-codex"

[[formation.slot]]
id = "slot_worker_a"
label = "Worker A"
controller = false
agentId = "worker-a"
harness = "openai-codex"
`)
}

func tmuxPeerBoardFixture() string {
	return `schema = 1
id = "brd_peer"
slug = "session-search"
title = "Peer Smoke"
rev = 1
updatedBy = "agent:test"
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_peer"
type = "peer"
title = "Peer proof pair"

[formation.brief]
goal = "Produce a peer synthesis from shared-plane contributions"
beadId = "home-21p5"

[[formation.input]]
id = "port_peer_in"
label = "Input"

[[formation.output]]
id = "port_peer_out"
label = "Output"

[[formation.slot]]
id = "slot_peer_a"
label = "Peer A"
controller = false
agentId = "peer-a"
harness = "openai-codex"

[[formation.slot]]
id = "slot_peer_b"
label = "Peer B"
controller = false
agentId = "peer-b"
harness = "openai-codex"
`
}

type fakeTmuxOp struct {
	op     string
	target string
}

// fakeWorkerPaneGone stands in a capture script for an observation that finds
// the session gone, so a test can make a worker session disappear part way
// through a run.
const fakeWorkerPaneGone = "\x00gone"

// fakeWorkerPaneReadError stands in a capture script for an observation that
// fails for a reason other than a missing target (a generic tmux read error),
// so a test can exercise the capture_failed defensive outcome.
const fakeWorkerPaneReadError = "\x00readerr"

// fakeWorkerPane scripts what Archon's read-only observation sees on one worker
// session: successive CapturePane results (the last one repeats) and, with
// missing set, a session that is absent from the first look onward.
type fakeWorkerPane struct {
	captures []string
	missing  bool
}

func (p *fakeWorkerPane) next() string {
	if len(p.captures) == 0 {
		return ""
	}
	text := p.captures[0]
	if len(p.captures) > 1 {
		p.captures = p.captures[1:]
	}
	return text
}

type fakeTmuxHarnessClient struct {
	harness              string
	sessions             []string
	pane                 tmuxPaneState
	noServer             bool
	hasServerErr         error
	startKeeperErr       error
	keepersStarted       []string
	listErr              error
	describeErr          error
	sendErr              error
	captureErr           error
	createErr            error
	sendErrEchoPrompt    bool
	captureErrEchoPrompt bool
	captures             []string
	artifact             string
	lastPrompt           string
	lastTarget           string
	lastDispatchID       string
	sentPrompts          []string
	sendTargets          []string
	paneText             string
	awaitingCapture      bool
	startupCaptures      []string
	readinessPending     map[string]bool
	preSendCaptureCalls  int
	lastPreSendCapture   string
	sendCalls            int
	captureCalls         int
	listCalls            int
	describeCalls        int
	created              []string
	killed               []string
	ops                  []fakeTmuxOp
	afterCapture         func(call int)
	afterSend            func(prompt string)
	// workerPanes is keyed by a fragment of the tmux target name. Owned session
	// names embed the slot id, so a test scripts a worker's pane by slot without
	// knowing the session nonce the executor generates.
	workerPanes map[string]*fakeWorkerPane
}

func (f *fakeTmuxHarnessClient) workerPane(target string) *fakeWorkerPane {
	for key, pane := range f.workerPanes {
		if strings.Contains(target, key) {
			return pane
		}
	}
	return nil
}

func fakeTmuxMissingTarget(target string) error {
	return fmt.Errorf("%w: can't find session: %s", errTmuxTargetMissing, target)
}

// liveSessions models the sessions currently visible on the socket: the
// pre-existing (foreign) fixtures plus every session the executor created and
// has not yet torn down.
func (f *fakeTmuxHarnessClient) liveSessions() []string {
	killed := make(map[string]int, len(f.killed))
	for _, name := range f.killed {
		killed[name]++
	}
	live := append([]string(nil), f.sessions...)
	for _, name := range f.created {
		if killed[name] > 0 {
			killed[name]--
			continue
		}
		live = append(live, name)
	}
	return live
}

func (f *fakeTmuxHarnessClient) HasServer(_ context.Context, _ string) (bool, error) {
	// Probe op carries no session target so safety assertions that scan ops for
	// foreign-target touches skip it.
	f.ops = append(f.ops, fakeTmuxOp{op: "has-server"})
	if f.hasServerErr != nil {
		return false, f.hasServerErr
	}
	return !f.noServer, nil
}

func (f *fakeTmuxHarnessClient) StartKeeper(_ context.Context, socket, keeper string) error {
	f.ops = append(f.ops, fakeTmuxOp{op: "start-keeper", target: keeper})
	if f.startKeeperErr != nil {
		return f.startKeeperErr
	}
	// Model tmux materializing the socket file when it starts the server, so the
	// executor's subsequent pinTmuxSocketIdentity (a real os.Lstat) succeeds.
	if strings.TrimSpace(socket) != "" {
		_ = os.WriteFile(socket, []byte("lazy-started tmux socket identity fixture"), 0o600)
	}
	f.keepersStarted = append(f.keepersStarted, keeper)
	f.sessions = append(f.sessions, keeper)
	f.noServer = false
	return nil
}

func (f *fakeTmuxHarnessClient) ListSessions(context.Context, string) ([]string, error) {
	f.listCalls++
	f.ops = append(f.ops, fakeTmuxOp{op: "list-sessions"})
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.liveSessions(), nil
}

func (f *fakeTmuxHarnessClient) CreateSession(_ context.Context, _, name, _, _ string) error {
	f.ops = append(f.ops, fakeTmuxOp{op: "new-session", target: name})
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, name)
	return nil
}

func (f *fakeTmuxHarnessClient) KillSession(_ context.Context, _, name string) error {
	f.ops = append(f.ops, fakeTmuxOp{op: "kill-session", target: name})
	f.killed = append(f.killed, name)
	return nil
}

func (f *fakeTmuxHarnessClient) DescribeActivePane(_ context.Context, _, target string) (tmuxPaneState, error) {
	f.describeCalls++
	f.ops = append(f.ops, fakeTmuxOp{op: "describe", target: target})
	if f.describeErr != nil {
		return tmuxPaneState{}, f.describeErr
	}
	if pane := f.workerPane(target); pane != nil && pane.missing {
		return tmuxPaneState{}, fakeTmuxMissingTarget(target)
	}
	if f.ownsTarget(target) {
		if f.readinessPending == nil {
			f.readinessPending = map[string]bool{}
		}
		f.readinessPending[target] = true
	}
	return f.pane, nil
}

func (f *fakeTmuxHarnessClient) SendPrompt(_ context.Context, _, target, dispatchID, prompt string) error {
	f.sendCalls++
	f.ops = append(f.ops, fakeTmuxOp{op: "send", target: target})
	f.lastDispatchID = dispatchID
	f.lastPrompt = prompt
	f.lastTarget = target
	f.sentPrompts = append(f.sentPrompts, prompt)
	f.sendTargets = append(f.sendTargets, target)
	f.awaitingCapture = true
	if f.afterSend != nil {
		f.afterSend(prompt)
	}
	if f.sendErrEchoPrompt {
		return fmt.Errorf("tmux send failed with prompt %s token=sk-tmu...t123", prompt)
	}
	return f.sendErr
}

func (f *fakeTmuxHarnessClient) ownsTarget(target string) bool {
	for _, created := range f.created {
		if created == target {
			return true
		}
	}
	return false
}

func (f *fakeTmuxHarnessClient) CapturePane(_ context.Context, _, target string, _ int) (string, error) {
	f.captureCalls++
	f.ops = append(f.ops, fakeTmuxOp{op: "capture", target: target})
	if f.afterCapture != nil {
		f.afterCapture(f.captureCalls)
	}
	workerPane := f.workerPane(target)
	if !f.awaitingCapture && f.readinessPending != nil && f.readinessPending[target] {
		captured := f.paneText
		if f.startupCaptures == nil {
			if f.harness == "claude-code" {
				captured = "Claude Code\n❯"
			} else {
				captured = "OpenAI Codex\n› Explain this codebase"
			}
		} else if len(f.startupCaptures) > 0 {
			captured = f.startupCaptures[0]
			if len(f.startupCaptures) > 1 {
				f.startupCaptures = f.startupCaptures[1:]
			}
		}
		f.preSendCaptureCalls++
		f.lastPreSendCapture = captured
		harness := f.harness
		if harness == "" {
			harness = "openai-codex"
		}
		if tmuxPaneShowsHarnessReady(harness, captured) {
			f.readinessPending[target] = false
		}
		return captured, nil
	}
	if workerPane != nil {
		if workerPane.missing {
			return "", fakeTmuxMissingTarget(target)
		}
		text := workerPane.next()
		if text == fakeWorkerPaneGone {
			return "", fakeTmuxMissingTarget(target)
		}
		if text == fakeWorkerPaneReadError {
			return "", fmt.Errorf("tmux read failed for %s: input/output error", target)
		}
		return text, nil
	}
	if !f.awaitingCapture {
		return f.paneText, nil
	}
	if f.captureErrEchoPrompt {
		return "", fmt.Errorf("tmux capture failed after prompt %s token=sk-tmu...t123", f.lastPrompt)
	}
	if f.captureErr != nil {
		return "", f.captureErr
	}
	var captured string
	if len(f.captures) > 0 {
		captured = strings.ReplaceAll(f.captures[0], "run_missing", runIDFromPrompt(f.lastPrompt))
		f.captures = f.captures[1:]
	} else {
		artifact := f.artifact
		if artifact == "" {
			artifact = "reports/tmux.md"
		}
		captured = fmt.Sprintf("agent output\n<<<CHROTE-DONE run-id=%s status=ok artifact=%s>>>\n", runIDFromPrompt(f.lastPrompt), artifact)
	}
	captured = withPromptOutputContract(captured, f.lastPrompt)
	f.awaitingCapture = false
	if f.paneText != "" {
		f.paneText += "\n"
	}
	f.paneText += captured
	return f.paneText, nil
}

func runIDFromPrompt(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		if runID, ok := strings.CutPrefix(strings.TrimSpace(line), "run: "); ok {
			return strings.TrimSpace(runID)
		}
	}
	return "run_missing"
}

func withPromptOutputContract(captured, prompt string) string {
	if !strings.Contains(prompt, "formation output contract:") || strings.Contains(captured, "```chrote-outputs") {
		return captured
	}
	ports := outputPortsFromPrompt(prompt)
	if len(ports) == 0 {
		return captured
	}
	payloads := make(map[string]FormationOutputPayload, len(ports))
	text := strings.TrimSpace(captured)
	if sentinelAt := strings.Index(text, "<<<CHROTE-DONE "); sentinelAt >= 0 {
		text = strings.TrimSpace(text[:sentinelAt])
	}
	for _, portID := range ports {
		payloads[portID] = FormationOutputPayload{Text: text}
	}
	raw, err := json.Marshal(payloads)
	if err != nil {
		return captured
	}
	block := "```chrote-outputs\n" + string(raw) + "\n```\n"
	if sentinelAt := strings.Index(captured, "<<<CHROTE-DONE "); sentinelAt >= 0 {
		return strings.TrimRight(captured[:sentinelAt], "\n") + "\n" + block + captured[sentinelAt:]
	}
	return strings.TrimRight(captured, "\n") + "\n" + block
}

func outputPortsFromPrompt(prompt string) []string {
	var ports []string
	seen := map[string]bool{}
	add := func(port string) {
		port = strings.TrimSpace(port)
		if port == "" || seen[port] {
			return
		}
		seen[port] = true
		ports = append(ports, port)
	}
	inList := false
	for _, line := range strings.Split(prompt, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "Use all and only these output port ids:":
			inList = true
			continue
		case inList && strings.HasPrefix(trimmed, "- "):
			fields := strings.Fields(strings.TrimPrefix(trimmed, "- "))
			if len(fields) > 0 {
				add(fields[0])
			}
			continue
		case inList && trimmed != "":
			inList = false
		}
		for cursor := 0; ; {
			idx := strings.Index(trimmed[cursor:], "\"port_")
			if idx < 0 {
				break
			}
			start := cursor + idx + 1
			endRel := strings.Index(trimmed[start:], "\"")
			if endRel < 0 {
				break
			}
			add(trimmed[start : start+endRel])
			cursor = start + endRel + 1
			if cursor >= len(trimmed) {
				break
			}
		}
	}
	return ports
}

type promptEchoDispatchAdapter struct{}

func (promptEchoDispatchAdapter) SendSlotDispatch(payload SlotDispatchPayload) error {
	return fmt.Errorf("adapter refused prompt %s token=sk-dispatchadapter123", payload.Prompt)
}

func assertLedgerDataForTypesRedacted(t *testing.T, events []RunEvent, eventTypes []string, forbidden ...string) {
	t.Helper()
	want := make(map[string]bool, len(eventTypes))
	seen := make(map[string]bool, len(eventTypes))
	for _, eventType := range eventTypes {
		want[eventType] = true
	}
	for _, event := range events {
		if !want[event.Type] {
			continue
		}
		seen[event.Type] = true
		raw, err := json.Marshal(event.Data)
		if err != nil {
			t.Fatalf("marshal event data for %s: %v", event.Type, err)
		}
		data := string(raw)
		for _, value := range forbidden {
			if strings.Contains(data, value) {
				t.Fatalf("%s data leaked %q: %s", event.Type, value, data)
			}
		}
	}
	for _, eventType := range eventTypes {
		if !seen[eventType] {
			t.Fatalf("events = %v, want redaction-checked event %s", eventTypesOf(events), eventType)
		}
	}
}

func firstStartedInputText(t *testing.T, events []RunEvent, nodeID string) string {
	t.Helper()
	for _, event := range events {
		if event.Type != RunEventNodeStarted || event.NodeID != nodeID {
			continue
		}
		inputs, ok := event.Data["inputRefs"].([]any)
		if !ok || len(inputs) == 0 {
			t.Fatalf("node_started %s inputRefs = %#v, want non-empty slice", nodeID, event.Data["inputRefs"])
		}
		return runInputRefFromAny(inputs[0]).Text
	}
	t.Fatalf("missing node_started for %s", nodeID)
	return ""
}

func eventTypesOf(events []RunEvent) []string {
	values := make([]string, 0, len(events))
	for _, event := range events {
		values = append(values, event.Type)
	}
	return values
}

// The excerpt must redact before truncating: cutting raw text first can split
// a secret-shaped token at the cap boundary, leaving a fragment the redaction
// patterns no longer match.
func TestWorkerObservationExcerptRedactsTokenStraddlingTheCapBoundary(t *testing.T) {
	raw := strings.Repeat("a", 40) + " REDACTED_TEST_SECRET"
	got, truncated := workerObservationExcerpt("", raw, 16)
	if !truncated {
		t.Fatalf("truncated = false, want true for a %d-byte delta with cap 16", len(raw))
	}
	if strings.Contains(got, "12345678") || strings.Contains(got, "abcdefgh") {
		t.Fatalf("excerpt = %q, contains fragments of the secret token", got)
	}
}

func TestWorkerOutcomeMappersDistinguishMissingSession(t *testing.T) {
	generic := fmt.Errorf("tmux read failed: input/output error")
	missing := fakeTmuxMissingTarget("owned-session")
	if got := workerBindOutcome(generic); got != workerOutcomeBindFailed {
		t.Fatalf("workerBindOutcome(generic) = %q, want %q", got, workerOutcomeBindFailed)
	}
	if got := workerBindOutcome(missing); got != workerOutcomeMissingSession {
		t.Fatalf("workerBindOutcome(missing) = %q, want %q", got, workerOutcomeMissingSession)
	}
	if got := workerCaptureOutcome(generic); got != workerOutcomeCaptureFailed {
		t.Fatalf("workerCaptureOutcome(generic) = %q, want %q", got, workerOutcomeCaptureFailed)
	}
	if got := workerCaptureOutcome(missing); got != workerOutcomeMissingSession {
		t.Fatalf("workerCaptureOutcome(missing) = %q, want %q", got, workerOutcomeMissingSession)
	}
}

// A completion capture that fails for a reason other than a missing target is
// the capture_failed defensive outcome: recorded as an anomaly on an otherwise
// successful run, never silence and never a run failure.

// reusedSeatReadiness runs the real readiness check over captured panes, and
// the fake client for everything else, so a peer formation exercises Ready the
// way a live daemon does when its facilitator reuses the first peer's seat.
type reusedSeatReadiness struct {
	*fakeTmuxHarnessClient
	real realSeatTransport
}

func (r *reusedSeatReadiness) Ready(ctx context.Context, socket string, s *nativeSeat, harness string) error {
	return r.real.Ready(ctx, socket, s, harness)
}

func TestPeerFacilitatorReusesTheFirstSeatAfterLongOutput(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	for _, id := range []string{"peer-a", "peer-b"} {
		createS4Persona(t, personas, id)
	}
	writeFixture(t, store.BoardPath("session-search"), tmuxPeerBoardFixture())
	cfg := tmuxTestConfig(t)
	client := &fakeTmuxHarnessClient{
		pane: tmuxPaneState{CurrentPath: cfg.Cwd},
		captures: []string{
			"PEER-A-BOUNDARY\n" + strings.Repeat("a long first turn\n", 150) + "<<<CHROTE-DONE run-id=run_missing status=ok artifact=peer-a.md>>>",
			"PEER-B-EVIDENCE\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=peer-b.md>>>",
			"PEER-COLLABORATED from PEER-A-BOUNDARY and PEER-B-EVIDENCE\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=peer-final.md>>>",
		},
	}
	startup, _, _ := paneFixture(t, "codex-idle-empty")
	startup = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(startup, "")
	history, err := os.ReadFile(filepath.Join("testdata", "panes", "codex-ready-capture-after-long-output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	idle, x, y := paneFixture(t, "codex-idle-after-long-output")
	readyChecks := map[string]int{}
	inputChecks := map[string]int{}
	target := func(args []string) string {
		for i, arg := range args {
			if arg == "-t" && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	events := make(chan struct{}, 64)
	for i := 0; i < cap(events); i++ {
		events <- struct{}{}
	}
	transport := &reusedSeatReadiness{fakeTmuxHarnessClient: client, real: realSeatTransport{
		control: func(context.Context, string, string) (*seatControl, error) { return &seatControl{events: events}, nil },
		command: func(_ context.Context, _ string, _ *strings.Reader, args ...string) (string, error) {
			switch {
			case args[0] == "display-message" && strings.Contains(args[len(args)-1], "cursor_x"):
				return fmt.Sprintf("%d %d 0", x, y), nil
			case args[0] == "display-message":
				return "0", nil
			case args[0] == "capture-pane" && hasString(args, "-e"):
				inputChecks[target(args)]++
				return idle, nil
			case args[0] == "capture-pane":
				// A fresh seat shows its banner; after its first turn the banner
				// has scrolled out of the captured history.
				readyChecks[target(args)]++
				if !hasString(client.sendTargets, target(args)) {
					return startup, nil
				}
				return string(history), nil
			}
			return "", fmt.Errorf("unexpected tmux command %v", args)
		},
	}}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
	executor.seatClient = transport
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunFormation("session-search", "fmn_peer", FormationRunRequest{Actor: "agent:test", Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 1}})
	if err != nil {
		t.Fatalf("run peer formation: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v after sends %v; the facilitator did not proceed on the reused seat", status, client.sendTargets)
	}
	if len(client.created) != 2 {
		t.Fatalf("created sessions = %v", client.created)
	}
	peerA, peerB := client.created[0], client.created[1]
	if got, want := client.sendTargets, []string{peerA, peerB, peerA}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("send targets = %v, want the facilitator on peer A's seat", got)
	}
	// Before their first turns the seats pass the banner check; the facilitator's
	// reused seat was found ready by its idle, empty input line instead.
	if readyChecks[peerA] == 0 || readyChecks[peerB] == 0 || inputChecks[peerA] != 1 || inputChecks[peerB] != 0 {
		t.Fatalf("banner checks %v, input checks %v", readyChecks, inputChecks)
	}
}
