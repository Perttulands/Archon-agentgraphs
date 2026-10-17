package formations

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestAttemptStopsFromRecordedLedgers(t *testing.T) {
	for _, tc := range []struct {
		name, cause string
		canceled    bool
	}{
		{"operator-cancel", "Paused by Perttu", true},
		{"output-cap", "exceeds configured output cap", false},
		{"seat-mismatch", "seat model/effort mismatch", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/attempt-stops/" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var events []RunEvent
			if err := json.Unmarshal(data, &events); err != nil {
				t.Fatal(err)
			}
			board := &BoardDocument{Formations: []FormationNode{{ID: events[0].NodeID}}}
			check := func(events []RunEvent, wantEnd string) {
				t.Helper()
				steps := projectRunAttempts("run_fixture", "", board, events, &RunStatusProjection{Final: true, Status: RunStatusCanceled, UpdatedAt: events[len(events)-1].Timestamp}, nil).Steps
				if len(steps) != 1 || len(steps[0].Attempts) != 1 {
					t.Fatalf("steps = %+v", steps)
				}
				attempt := steps[0].Attempts[0]
				if attempt.Status != AttemptStopped || attempt.StopReason == nil || !strings.Contains(attempt.StopReason.Text, tc.cause) || attempt.EndedAt != wantEnd {
					t.Fatalf("attempt = %+v, reason = %+v, want end %s", attempt, attempt.StopReason, wantEnd)
				}
			}
			wantEnd := events[len(events)-2].Timestamp
			if tc.canceled {
				wantEnd = events[len(events)-1].Timestamp
			}
			check(events, wantEnd)
			if tc.canceled {
				// A cancellation may reach the ledger without executor cleanup first.
				check(append(events[:1:1], events[len(events)-1]), wantEnd)
			}
		})
	}
}
