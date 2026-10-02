package coordinator

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// An oracle over generated missions (archon-o7p.11): branches, human gates
// sending work back or forward, routes meeting in one port, and verdicts
// given while other steps work. Each mission runs to its end, then restarts
// after every event; every restart must end the same way with each step
// having run on the same inputs. The seeds are fixed, so a failure names its
// mission.

// randomGateBoard builds a valid mission from rng: two to four steps fed in
// order, one or two human gates judging distinct steps, every route leading
// to a later step, back to an earlier one or to an End node.
func randomGateBoard(rng *rand.Rand) (restartCase, bool) {
	steps := 2 + rng.Intn(3)
	gates := 1 + rng.Intn(2)
	if gates > steps {
		gates = steps
	}
	step := func(i int) string { return fmt.Sprintf("fmn_s%d", i) }
	gate := func(i int) string { return fmt.Sprintf("gate_g%d", i) }
	var body, wires strings.Builder
	for i := range steps {
		body.WriteString(gateBoardFormation(step(i)))
	}
	for i := range gates {
		body.WriteString(gateBoardHumanGate(gate(i)))
	}
	body.WriteString(endNodes)
	edge := 0
	wire := func(from, to string) {
		edge++
		fmt.Fprintf(&wires, "[[connection]]\nid = \"edge_%d\"\nfrom = \"%s\"\nto = \"%s\"\n", edge, from, to)
	}
	fed := map[int]bool{0: true}
	wire("mis_proof:out", step(0)+":port_in")
	if steps > 1 && rng.Intn(2) == 0 {
		wire("mis_proof:out", step(1)+":port_in")
		fed[1] = true
	}
	judged := rng.Perm(steps)[:gates]
	gateOf := map[int]int{}
	for g, s := range judged {
		gateOf[s] = g
	}
	later := func(from int) (int, bool) {
		if from+1 >= steps {
			return 0, false
		}
		return from + 1 + rng.Intn(steps-from-1), true
	}
	for s := range steps {
		if g, ok := gateOf[s]; ok {
			wire(step(s)+":port_out", gate(g)+":in")
			if next, ok := later(s); ok && rng.Intn(2) == 0 {
				wire(gate(g)+":pass", step(next)+":port_in")
				fed[next] = true
			} else {
				wire(gate(g)+":pass", "end_done:in")
			}
			switch rng.Intn(4) {
			case 0:
				wire(gate(g)+":fail", step(rng.Intn(s+1))+":port_in")
			case 1, 2:
				if next, ok := later(s); ok {
					wire(gate(g)+":fail", step(next)+":port_in")
					fed[next] = true
				} else {
					wire(gate(g)+":fail", "end_rejected:in")
				}
			default:
				wire(gate(g)+":fail", "end_rejected:in")
			}
			continue
		}
		if next, ok := later(s); ok && rng.Intn(3) != 0 {
			wire(step(s)+":port_out", step(next)+":port_in")
			fed[next] = true
		} else {
			wire(step(s)+":port_out", "end_done:in")
		}
	}
	for s := range steps {
		if !fed[s] {
			wire("mis_proof:out", step(s)+":port_in")
		}
	}
	tc := restartCase{board: gateBoard(body.String() + wires.String()), verdicts: map[string][]string{}, answerWhile: map[string][]string{}}
	for g := range gates {
		var script []string
		for range rng.Intn(3) {
			script = append(script, "fail")
		}
		tc.verdicts[gate(g)] = append(script, "pass")
	}
	// Most gates are answered while a later step works, when that step runs
	// after the gate asks.
	for g, s := range judged {
		if next, ok := later(s); ok && rng.Intn(4) != 0 {
			tc.answerWhile[step(next)] = append(tc.answerWhile[step(next)], gate(g))
		}
	}
	return tc, true
}

// validMission reports whether the store reads the mission with no errors.
func validMission(t *testing.T, board string) bool {
	t.Helper()
	store := formations.NewStore(t.TempDir())
	writeState(t, store.BoardPath("proof"), []byte(board))
	document, err := store.ReadBoard("proof")
	return err == nil && len(formations.ValidateBoard(document).Errors) == 0
}

func TestRandomMissionsReachTheSameOutcomeAfterAnyRestart(t *testing.T) {
	// ARCHON_ORACLE_MISSIONS widens the sweep beyond the sixteen CI runs.
	want := 16
	if n, err := strconv.Atoi(os.Getenv("ARCHON_ORACLE_MISSIONS")); err == nil && n > 0 {
		want = n
	}
	boards := 0
	for seed := int64(1); boards < want && seed < int64(want)*40; seed++ {
		tc, _ := randomGateBoard(rand.New(rand.NewSource(seed)))
		if !validMission(t, tc.board) {
			continue
		}
		boards++
		tc.name = fmt.Sprintf("seed %d", seed)
		// A step may run before the gate it answers asks, so a verdict while
		// it works is likely here, not required.
		tc.midStepOptional = true
		t.Run(tc.name, func(t *testing.T) { checkRestartCuts(t, tc) })
	}
	if boards < want {
		t.Fatalf("only %d valid missions", boards)
	}
}
