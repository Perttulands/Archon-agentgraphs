package formations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func peerTestStore(t *testing.T) (*Store, PeerConversationID) {
	t.Helper()
	store := NewStore(t.TempDir())
	id := PeerConversationID{RunID: "run_peer_test", NodeID: "fmn_peers", Attempt: 1}
	peerTestRun(t, store, id.RunID)
	return store, id
}

func peerTestRun(t *testing.T, store *Store, runID string) {
	t.Helper()
	path := filepath.Join(store.Workspace, ".formations", "runs", "peer-tests", runID+".ndjson")
	if err := writeInitialRunEvent(path, RunEvent{RunID: runID, Seq: 1, Type: RunEventStarted, Timestamp: store.now().Format(time.RFC3339Nano), Actor: "test"}); err != nil {
		t.Fatal(err)
	}
}

func createPeerTestConversation(t *testing.T, store *Store, id PeerConversationID, deadline time.Time) string {
	t.Helper()
	path, err := store.CreatePeerConversation(FormationExecution{RunID: id.RunID, NodeID: id.NodeID, Attempt: id.Attempt}, []string{"slot_a", "slot_b", "slot_c"}, map[string]string{
		"slot_a": "Independent opening A", "slot_b": "Independent opening B", "slot_c": "Independent opening C",
	}, deadline)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(store.Workspace, ".formations", "artifacts", id.RunID, path)
}

func peerAppend(t *testing.T, store *Store, id PeerConversationID, request PeerAppendRequest) *PeerConversation {
	t.Helper()
	state, err := store.AppendPeerConversation(id, request)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPeerConversationAgreementRequiresEveryParticipantAndCurrentProposal(t *testing.T) {
	store, id := peerTestStore(t)
	createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
	state, err := store.ReadPeerConversation(id)
	if err != nil || state.LastSeq != 3 || state.Status != "open" || len(state.Entries) != 3 {
		t.Fatalf("independent openings: %+v %v", state, err)
	}
	for index, slot := range state.Participants {
		if state.Entries[index].Kind != "opening" || state.Entries[index].SlotID != slot {
			t.Fatalf("opening attribution: %+v", state.Entries)
		}
	}
	first := peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_b", Kind: "proposal", Text: "Proposed conclusion"}).LastSeq
	peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_a", Kind: "ack", ProposalSeq: first})
	peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_c", Kind: "dissent", ProposalSeq: first, Text: "Evidence does not settle the deadline"})
	if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_b", Kind: "ack", ProposalSeq: first}); !errors.Is(err, ErrPeerProposalConflict) {
		t.Fatalf("contested proposal accepted: %v", err)
	}
	finalText := "Agreed findings. Unresolved tension: deadline evidence conflicts.\n```chrote-outputs\n{\"out\":{\"text\":\"Ask the operator to settle the deadline\"}}\n```"
	state = peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_c", Kind: "proposal", Text: finalText})
	latest := state.LastSeq
	if len(state.Proposal.Acknowledged) != 0 || state.Proposal.Contested {
		t.Fatalf("new proposal retained old consent: %+v", state.Proposal)
	}
	if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_b", Kind: "ack", ProposalSeq: first}); !errors.Is(err, ErrPeerProposalConflict) {
		t.Fatalf("stale acknowledgement accepted: %v", err)
	}
	peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_a", Kind: "ack", ProposalSeq: latest})
	state = peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: "The tension is now stated accurately."})
	if len(state.Proposal.Acknowledged) != 1 {
		t.Fatal("ordinary message discarded consent")
	}
	peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_b", Kind: "ack", ProposalSeq: latest})
	state = peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_c", Kind: "ack", ProposalSeq: latest})
	if state.Status != "agreed" || state.FinalText != finalText || len(state.Proposal.Acknowledged) != 3 {
		t.Fatalf("final agreement: %+v", state)
	}
	if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_a", Kind: "message", Text: "too late"}); !errors.Is(err, ErrPeerConversationClosed) {
		t.Fatalf("append after agreement: %v", err)
	}
	restarted, err := NewStore(store.Workspace).ReadPeerConversation(id)
	if err != nil || !reflect.DeepEqual(restarted, state) {
		t.Fatalf("restart lost agreement: %+v %v", restarted, err)
	}
}

func TestPeerConversationIsolationAndExclusiveCreation(t *testing.T) {
	store, first := peerTestStore(t)
	ids := []PeerConversationID{first, {RunID: first.RunID, NodeID: "fmn_other", Attempt: 1}, {RunID: first.RunID, NodeID: first.NodeID, Attempt: 2}, {RunID: "run_other", NodeID: first.NodeID, Attempt: 1}}
	peerTestRun(t, store, "run_other")
	paths := map[string]bool{}
	for _, id := range ids {
		path := createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
		if paths[path] {
			t.Fatal("attempt journals share a path")
		}
		paths[path] = true
		peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_a", Kind: "message", Text: fmt.Sprint(id)})
	}
	for _, id := range ids {
		state, err := store.ReadPeerConversation(id)
		if err != nil || state.LastSeq != 4 || state.Entries[3].Text != fmt.Sprint(id) {
			t.Fatalf("identity crossed: %+v %v", state, err)
		}
	}
	before, _ := store.ReadPeerConversation(first)
	_, err := store.CreatePeerConversation(FormationExecution{RunID: first.RunID, NodeID: first.NodeID, Attempt: 1}, []string{"a", "b"}, map[string]string{"a": "replacement", "b": "replacement"}, store.now().Add(time.Hour))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced existing attempt: %v", err)
	}
	after, _ := store.ReadPeerConversation(first)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("duplicate create replenished deadline or replaced evidence")
	}
}

func TestPeerConversationConcurrentWritersAndReaders(t *testing.T) {
	store, id := peerTestStore(t)
	createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
	var workers sync.WaitGroup
	errs := make(chan error, 12)
	for worker := 0; worker < 12; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			other := NewStore(store.Workspace)
			for message := 0; message < 10; message++ {
				if _, err := other.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: fmt.Sprintf("writer %d message %d", worker, message)}); err != nil {
					errs <- err
					return
				}
				if _, err := other.ReadPeerConversation(id); err != nil {
					errs <- err
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	state, err := store.ReadPeerConversation(id)
	if err != nil || state.LastSeq != 123 {
		t.Fatalf("lost/interleaved messages: %+v %v", state, err)
	}
	seen := map[string]bool{}
	for _, entry := range state.Entries[3:] {
		if seen[entry.Text] || entry.SlotID != "slot_b" {
			t.Fatalf("duplicate or unattributed message: %+v", entry)
		}
		seen[entry.Text] = true
	}
}

func TestPeerConversationProcessWriter(t *testing.T) {
	if os.Getenv("ARCHON_PEER_TEST_CHILD") != "1" {
		return
	}
	args := os.Args[len(os.Args)-2:]
	store := NewStore(args[0])
	id := PeerConversationID{RunID: "run_peer_test", NodeID: "fmn_peers", Attempt: 1}
	for index := 0; index < 8; index++ {
		if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_a", Kind: "message", Text: fmt.Sprintf("%s-%d", args[1], index)}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPeerConversationLocksAcrossProcesses(t *testing.T) {
	store, id := peerTestStore(t)
	createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
	commands := make([]*exec.Cmd, 4)
	outputs := make([]bytes.Buffer, len(commands))
	for index := range commands {
		command := exec.Command(os.Args[0], "-test.run=^TestPeerConversationProcessWriter$", "--", store.Workspace, fmt.Sprint(index))
		command.Env = append(os.Environ(), "ARCHON_PEER_TEST_CHILD=1")
		command.Stdout, command.Stderr = &outputs[index], &outputs[index]
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands[index] = command
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("writer: %v %s", err, outputs[index].String())
		}
	}
	state, err := store.ReadPeerConversation(id)
	if err != nil || state.LastSeq != 35 {
		t.Fatalf("cross-process journal: %+v %v", state, err)
	}
}

func TestPeerConversationDeadlineCancellationAndRestart(t *testing.T) {
	store, id := peerTestStore(t)
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	store.Now = func() time.Time { return now }
	deadline := now.Add(37 * time.Second)
	createPeerTestConversation(t, store, id, deadline)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.WaitPeerConversation(ctx, id, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait ignored cancellation: %v", err)
	}
	now = deadline
	if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_b", Kind: "proposal", Text: "late result"}); !errors.Is(err, ErrPeerDeadlineExceeded) {
		t.Fatalf("late proposal accepted: %v", err)
	}
	restarted := NewStore(store.Workspace)
	restarted.Now = store.Now
	state, err := restarted.ReadPeerConversation(id)
	if err != nil || state.Status != "expired" || !state.Deadline.Equal(deadline) || state.LastSeq != 3 {
		t.Fatalf("restart replenished budget or lost partial work: %+v %v", state, err)
	}
	state, err = restarted.WaitPeerConversation(context.Background(), id, 3)
	if !errors.Is(err, ErrPeerDeadlineExceeded) || state.Status != "expired" {
		t.Fatalf("wait after deadline: %+v %v", state, err)
	}
	state, err = restarted.ClosePeerConversation(id, "deadline reached without agreed valid output")
	if err != nil || state.Status != "closed" || state.LastSeq != 4 || state.Reason == "" {
		t.Fatalf("expiry closure: %+v %v", state, err)
	}
	if _, err := restarted.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_a", Kind: "message", Text: "late"}); !errors.Is(err, ErrPeerConversationClosed) {
		t.Fatalf("closed append: %v", err)
	}
}

func TestPeerConversationWaitWakesForAppendAndCanBeCanceled(t *testing.T) {
	store, id := peerTestStore(t)
	createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		state *PeerConversation
		err   error
	}
	completed := make(chan result, 1)
	watching := make(chan struct{})
	var watched sync.Once
	reader := NewStore(store.Workspace)
	reader.Now = func() time.Time {
		// Wait first reads the clock under the read lock after installing its
		// watcher. The append below waits for that read lock to release.
		watched.Do(func() { close(watching) })
		return time.Now().UTC()
	}
	go func() { state, err := reader.WaitPeerConversation(ctx, id, 3); completed <- result{state, err} }()
	<-watching
	peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: "Responding to opening A"})
	got := <-completed
	if got.err != nil || got.state.LastSeq != 4 || got.state.Entries[3].Text != "Responding to opening A" {
		t.Fatalf("missed wakeup: %+v %v", got.state, got.err)
	}
	ctx2, stop := context.WithCancel(ctx)
	watching2 := make(chan struct{})
	reader2 := NewStore(store.Workspace)
	var watched2 sync.Once
	reader2.Now = func() time.Time {
		watched2.Do(func() { close(watching2) })
		return time.Now().UTC()
	}
	go func() { state, err := reader2.WaitPeerConversation(ctx2, id, 4); completed <- result{state, err} }()
	<-watching2
	stop()
	if got = <-completed; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("blocked wait ignored cancellation: %v", got.err)
	}
}

func TestPeerConversationPinnedDirectoryAndUnsafeFileSubstitution(t *testing.T) {
	store, id := peerTestStore(t)
	path := createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
	directory, err := store.openPeerDirectory(id, false)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.close()
	original := filepath.Dir(path)
	renamed := original + "-moved"
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, original); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendPeerConversationAt(directory, id, PeerAppendRequest{SlotID: "slot_a", Kind: "message", Text: "still pinned"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, peerConversationFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escaped into replacement directory: %v", err)
	}
	if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: "unsafe"}); err == nil {
		t.Fatal("followed a substituted directory symlink")
	}

	for _, mode := range []string{"symlink", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			otherStore, otherID := peerTestStore(t)
			otherPath := createPeerTestConversation(t, otherStore, otherID, otherStore.now().Add(time.Minute))
			raw, err := os.ReadFile(otherPath)
			if err != nil {
				t.Fatal(err)
			}
			outsideFile := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outsideFile, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(otherPath); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if mode == "hardlink" {
				link = os.Link
			}
			if err := link(outsideFile, otherPath); err != nil {
				t.Fatal(err)
			}
			if _, err := otherStore.ReadPeerConversation(otherID); err == nil {
				t.Fatal("read unsafe file")
			}
			if _, err := otherStore.AppendPeerConversation(otherID, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: "unsafe"}); err == nil {
				t.Fatal("appended unsafe file")
			}
			after, _ := os.ReadFile(outsideFile)
			if !bytes.Equal(raw, after) {
				t.Fatal("outside file modified")
			}
		})
	}
}

func TestPeerConversationInvalidAndOversizeWritesLeaveEvidenceUntouched(t *testing.T) {
	store, id := peerTestStore(t)
	path := createPeerTestConversation(t, store, id, store.now().Add(time.Minute))
	for _, request := range []PeerAppendRequest{
		{SlotID: "other", Kind: "message", Text: "intruder"},
		{SlotID: "slot_a", Kind: "opening", Text: "replacement"},
		{SlotID: "slot_a", Kind: "ack", ProposalSeq: 3},
		{SlotID: "slot_a", Kind: "closed", Text: "agent must not close"},
		{SlotID: "slot_a", Kind: "message", Text: strings.Repeat("x", PeerMessageMaxBytes+1)},
	} {
		before, _ := os.ReadFile(path)
		if _, err := store.AppendPeerConversation(id, request); err == nil {
			t.Fatalf("accepted invalid request: %+v", request.Kind)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("invalid append changed journal")
		}
	}
	large := strings.Repeat("x", PeerMessageMaxBytes)
	for range 3 {
		peerAppend(t, store, id, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: large})
	}
	before, _ := os.ReadFile(path)
	if _, err := store.AppendPeerConversation(id, PeerAppendRequest{SlotID: "slot_b", Kind: "message", Text: large}); err == nil {
		t.Fatal("accepted total byte overflow")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("overflow partially appended a record")
	}
	if _, err := store.ReadPeerConversation(id); err != nil {
		t.Fatalf("overflow damaged readable evidence: %v", err)
	}
	for _, bad := range []PeerConversationID{{RunID: id.RunID, NodeID: "../escape", Attempt: 1}, {RunID: id.RunID, NodeID: id.NodeID, Attempt: 0}, {RunID: "../run_bad", NodeID: id.NodeID, Attempt: 1}} {
		if _, err := store.ReadPeerConversation(bad); err == nil {
			t.Fatalf("accepted unsafe identity: %+v", bad)
		}
	}
}
