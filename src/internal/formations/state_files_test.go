package formations

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// A ledger is read only when it is a valid sequence of this run's events.
func TestRunLedgerReadsOnlyValidEventSequences(t *testing.T) {
	event := func(seq, runID, extra string) string {
		return `{"ts":"2026-10-01T12:00:00Z","runId":"` + runID + `","seq":` + seq + `,"type":"run_started","actor":"agent:test"` + extra + "}\n"
	}
	runID := newPrefixedID("run")
	for name, ledger := range map[string]string{
		"empty":               "",
		"blank line":          event("1", runID, "") + "\n" + event("2", runID, ""),
		"not JSON":            "{\n",
		"sequence gap":        event("1", runID, "") + event("3", runID, ""),
		"sequence from zero":  event("0", runID, ""),
		"another run":         event("1", runID, "") + event("2", "run_other", ""),
		"missing actor":       strings.Replace(event("1", runID, ""), `,"actor":"agent:test"`, "", 1),
		"bad timestamp":       strings.Replace(event("1", runID, ""), "2026-10-01T12:00:00Z", "yesterday", 1),
		"oversized event":     event("1", runID, `,"data":{"note":"`+strings.Repeat("x", runEventMaxBytes)+`"}`),
		"invalid UTF-8 event": event("1", runID, `,"data":{"note":"`+"\xff"+`"}`),
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := s4RunFixture(t)
			writeFixture(t, filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson")), ledger)
			if events, err := store.ReadRunEvents(runID); !errors.Is(err, ErrRunLedgerInvalid) {
				t.Fatalf("ReadRunEvents = %v, %v; want ErrRunLedgerInvalid", events, err)
			}
		})
	}

	store, _ := s4RunFixture(t)
	// Fields Archon does not know, such as those an older writer added, are
	// ignored rather than refused.
	writeFixture(t, filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson")), event("1", runID, `,"schema":2`)+event("2", runID, ""))
	if events, err := store.ReadRunEvents(runID); err != nil || len(events) != 2 {
		t.Fatalf("ReadRunEvents = %v, %v; want both events", events, err)
	}
}
