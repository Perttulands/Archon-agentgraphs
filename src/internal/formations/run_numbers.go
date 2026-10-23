package formations

import (
	"cmp"
	"slices"
	"time"
)

// NumberRuns gives each run its place among its mission's runs in start
// order, from 1, so the CLI and the cockpit call a run by the same number
// (archon-kiwt.3). A mission is its identity: a mission created again under a
// deleted one's slug counts its own runs. Runs that started at the same
// instant are ordered by run ID. The number is derived, never stored, so runs
// must hold every readable run of each mission it numbers.
func NumberRuns(runs []*RunStatusProjection) {
	ordered := slices.Clone(runs)
	slices.SortStableFunc(ordered, func(a, b *RunStatusProjection) int {
		return cmp.Or(runStart(a).Compare(runStart(b)), cmp.Compare(a.RunID, b.RunID))
	})
	counts := map[string]int{}
	for _, run := range ordered {
		counts[run.BoardID]++
		run.Number = counts[run.BoardID]
	}
}

func runStart(run *RunStatusProjection) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, run.StartedAt)
	return at
}
