package formations

import "testing"

// Runs are numbered per mission identity in start order; a tie in start time
// goes by run ID (archon-kiwt.3).
func TestNumberRunsCountsEachMissionInStartOrder(t *testing.T) {
	runs := []*RunStatusProjection{
		{RunID: "run_c", BoardID: "brd_a", StartedAt: "2026-10-02T09:00:00Z"},
		{RunID: "run_b", BoardID: "brd_a", StartedAt: "2026-10-01T09:00:00.5Z"},
		{RunID: "run_a", BoardID: "brd_a", StartedAt: "2026-10-01T09:00:00.5Z"},
		{RunID: "run_x", BoardID: "brd_b", StartedAt: "2026-10-03T09:00:00Z"},
		{RunID: "run_d", BoardID: "brd_a", StartedAt: "2026-10-01T09:00:00.25Z"},
	}
	NumberRuns(runs)
	want := map[string]int{"run_d": 1, "run_a": 2, "run_b": 3, "run_c": 4, "run_x": 1}
	for _, run := range runs {
		if run.Number != want[run.RunID] {
			t.Errorf("%s is number %d, want %d", run.RunID, run.Number, want[run.RunID])
		}
	}
}
