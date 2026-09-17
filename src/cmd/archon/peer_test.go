package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func peerCLIFixture(t *testing.T) (*formations.Store, formations.PeerConversationID) {
	t.Helper()
	store := formations.NewStore(t.TempDir())
	id := formations.PeerConversationID{RunID: "run_peer_cli", NodeID: "fmn_chat", Attempt: 2}
	event := formations.RunEvent{RunID: id.RunID, Seq: 1, Type: formations.RunEventStarted, Timestamp: store.Now().Format(time.RFC3339Nano), Actor: "test"}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	writeArchonFile(t, filepath.Join(store.Workspace, ".formations", "runs", "peer-cli", id.RunID+".ndjson"), string(raw)+"\n")
	_, err = store.CreatePeerConversation(formations.FormationExecution{RunID: id.RunID, NodeID: id.NodeID, Attempt: id.Attempt}, []string{"slot_a", "slot_b"}, map[string]string{"slot_a": "Opening A", "slot_b": "Opening B"}, store.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return store, id
}

func peerCLIArgs(id formations.PeerConversationID) []string {
	return []string{"--run", id.RunID, "--node", id.NodeID, "--attempt", strconv.Itoa(id.Attempt)}
}

func runPeerCLI(t *testing.T, store *formations.Store, id formations.PeerConversationID, command string, extra ...string) *formations.PeerConversation {
	t.Helper()
	args := append([]string{"--workspace", store.Workspace, "peer", command}, peerCLIArgs(id)...)
	args = append(args, extra...)
	stdout, stderr, code := runArchon(t, &fakeTmux{}, args...)
	if code != 0 || stderr != "" {
		t.Fatalf("peer %s: code=%d stderr=%s stdout=%s", command, code, stderr, stdout)
	}
	var result formations.PeerConversation
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	return &result
}

func TestArchonPeerCommandsExchangeMessagesAndAgree(t *testing.T) {
	store, id := peerCLIFixture(t)
	state := runPeerCLI(t, store, id, "read", "--json")
	if state.Status != "open" || state.LastSeq != 2 || len(state.Entries) != 2 {
		t.Fatalf("read: %+v", state)
	}
	message := "I read both openings.\nKeep these words exactly: $HOME `literal` and ää."
	messagePath := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(messagePath, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	state = runPeerCLI(t, store, id, "post", "--slot", "slot_b", "--text-file", messagePath)
	if state.LastSeq != 3 || state.Entries[2].Text != message {
		t.Fatalf("message changed: %+v", state)
	}
	state = runPeerCLI(t, store, id, "wait", "--after", "2", "--slot", "slot_a")
	if state.LastSeq != 3 {
		t.Fatalf("wait missed message: %+v", state)
	}
	state = runPeerCLI(t, store, id, "propose", "--slot", "slot_b", "--text", "First conclusion")
	proposal := strconv.Itoa(state.LastSeq)
	state = runPeerCLI(t, store, id, "post", "--slot", "slot_a", "--dissent", "--proposal", proposal, "--text", "This loses our unresolved tension")
	if !state.Proposal.Contested || state.Status != "open" {
		t.Fatalf("dissent not retained: %+v", state)
	}
	final := "We agree the deadline is unresolved.\n```chrote-outputs\n{\"out\":{\"text\":\"Ask the operator which deadline to use\"}}\n```"
	if err := os.WriteFile(messagePath, []byte(final), 0600); err != nil {
		t.Fatal(err)
	}
	state = runPeerCLI(t, store, id, "propose", "--slot", "slot_a", "--text-file", messagePath)
	proposal = strconv.Itoa(state.LastSeq)
	state = runPeerCLI(t, store, id, "ack", "--slot", "slot_a", "--proposal", proposal)
	if state.Status != "open" {
		t.Fatal("proposer alone finalized the conversation")
	}
	state = runPeerCLI(t, store, id, "ack", "--slot", "slot_b", "--proposal", proposal)
	if state.Status != "agreed" || state.FinalText != final {
		t.Fatalf("agreement lost text: %+v", state)
	}
	state = runPeerCLI(t, store, id, "read")
	if state.Status != "agreed" || state.FinalText != final {
		t.Fatalf("final history unavailable: %+v", state)
	}
}

func TestArchonPeerRejectsAmbiguousOrUnattributedWrites(t *testing.T) {
	store, id := peerCLIFixture(t)
	for _, extra := range [][]string{
		{"post", "--text", "no author"},
		{"post", "--slot", "slot_a", "--text", "ambiguous", "--text-file", "other.txt"},
		{"post", "--slot", "slot_a", "--dissent", "--text", "no exact proposal"},
		{"propose", "--slot", "slot_a", "--text", "result", "--after", "3"},
		{"ack", "--slot", "slot_a", "--proposal", "4", "--text", "ambiguous"},
		{"read", "--text", "not a read"},
	} {
		args := append([]string{extra[0]}, peerCLIArgs(id)...)
		args = append(args, extra[1:]...)
		var stdout, stderr bytes.Buffer
		if code := runPeer(context.Background(), store, args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("accepted %v: code=%d stdout=%s stderr=%s", extra, code, &stdout, &stderr)
		}
	}
	state, err := store.ReadPeerConversation(id)
	if err != nil || state.LastSeq != 2 {
		t.Fatalf("invalid command wrote evidence: %+v %v", state, err)
	}
}

func TestArchonPeerExpiredWaitReturnsEvidenceAndNonzeroStatus(t *testing.T) {
	store, id := peerCLIFixture(t)
	initial, err := store.ReadPeerConversation(id)
	if err != nil {
		t.Fatal(err)
	}
	store.Now = func() time.Time { return initial.Deadline }
	args := append([]string{"wait"}, peerCLIArgs(id)...)
	args = append(args, "--after", "2")
	var stdout, stderr bytes.Buffer
	code := runPeer(context.Background(), store, args, &stdout, &stderr)
	var state formations.PeerConversation
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if code != 1 || state.Status != "expired" || state.LastSeq != 2 || !strings.Contains(stderr.String(), "deadline exceeded") {
		t.Fatalf("expired wait: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}
