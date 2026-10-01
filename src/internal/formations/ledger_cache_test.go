package formations

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The ledger cache (archon-o7p.19) parses only appended lines, yet every read
// equals a full parse of the file as it stands.

func cachedLedgerRun(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, _, _, status := startBranchingRun(t, branchingGateBoardFixture(), &fakeRunExecutor{})
	return store, status.RunID, filepath.Join(store.Workspace, runArtifactPath("session-search", status.RunID, ".ndjson"))
}

func fullParse(t *testing.T, path, runID string) []RunEvent {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	events, err := readRunEventsFrom(file, runID)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestTheLedgerCacheReadsWhatAFullParseReads(t *testing.T) {
	store, runID, path := cachedLedgerRun(t)
	first := mustEvents(t, store, runID)
	if !reflect.DeepEqual(first, fullParse(t, path, runID)) {
		t.Fatal("a cached read differs from a full parse")
	}
	if err := store.AppendRunEvent(runID, RunEvent{Type: RunEventSeatState, NodeID: "fmn_b", SlotID: "slot_b", Data: map[string]any{"state": SeatStateWorking}}); err != nil {
		t.Fatal(err)
	}
	second := mustEvents(t, store, runID)
	if len(second) != len(first)+1 || !reflect.DeepEqual(second, fullParse(t, path, runID)) {
		t.Fatalf("after an append: %d events, want %d and a full parse's", len(second), len(first)+1)
	}
	// A slice already handed out never grows into the cache.
	grown := append(first, RunEvent{Type: "mine"})
	if again := mustEvents(t, store, runID); again[len(first)].Type == "mine" || grown[len(first)].Type != "mine" {
		t.Fatal("a caller's append reached the cache")
	}
}

func TestTheLedgerCacheRereadsALedgerThatChanged(t *testing.T) {
	store, runID, path := cachedLedgerRun(t)
	events := mustEvents(t, store, runID)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rewritten in place without its last event: shorter, so read again.
	lines := bytes.SplitAfter(raw, []byte{'\n'})
	shorter := bytes.Join(lines[:len(lines)-2], nil)
	if err := os.WriteFile(path, shorter, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mustEvents(t, store, runID); len(got) != len(events)-1 {
		t.Fatalf("after a shorter rewrite: %d events, want %d", len(got), len(events)-1)
	}
	// Rewritten to the same length with a different event: read again.
	last := bytes.LastIndex(raw, []byte(`"actor":"agent:test"`))
	if last < 0 {
		t.Fatal("fixture: no event has the agent:test actor")
	}
	edited := append(append(append([]byte(nil), raw[:last]...), []byte(`"actor":"agent:tset"`)...), raw[last+len(`"actor":"agent:test"`):]...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mustEvents(t, store, runID)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mustEvents(t, store, runID); !reflect.DeepEqual(got, fullParse(t, path, runID)) || got[len(got)-1].Actor == got[0].Actor && !bytes.Contains(edited, []byte("agent:tset")) {
		t.Fatal("after a same-length rewrite the cache still read the old ledger")
	}
}

// A half-written last line is not yet an event to a reader without the lock;
// a reader holding the lock fails on it, as a full parse does.
func TestAHalfWrittenLineWaitsForItsEnd(t *testing.T) {
	store, runID, path := cachedLedgerRun(t)
	events := mustEvents(t, store, runID)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(`{"seq":`); err != nil {
		t.Fatal(err)
	}
	board, err := store.ReadRunBoard(runID)
	if err != nil || board == nil {
		t.Fatalf("an unlocked read failed on a half-written line: %v", err)
	}
	if _, err := store.ReadRunEvents(runID); !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("a locked read of a torn line = %v, want invalid", err)
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
}
