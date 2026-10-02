package coordinator

// A wider oracle than random_boards_test.go (archon-o7p.11), first written by
// the rv-engine3 review: judge gates, joins of two ports, fan-out beside a
// gate, gate passes into later steps carrying a response, and answers given
// while judges work. Each mission runs to its end, then restarts after every
// event; every restart must end the same way, each step having run on the
// same inputs, and every ledger must keep richInvariants. Sixteen missions
// run in CI; ARCHON_ORACLE_MISSIONS widens the sweep, as for the narrower
// oracle.

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func richFormation(id string, ports []string) string {
	s := `
[[formation]]
id = "` + id + `"
type = "solo"
title = "` + strings.ToUpper(strings.TrimPrefix(id, "fmn_")) + `"
`
	for _, p := range ports {
		s += "[[formation.input]]\nid = \"" + p + "\"\nlabel = \"" + p + "\"\n"
	}
	s += `[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_` + id + `"
label = "Worker"
agentId = "builder"
harness = "openai-codex"
effort = "medium"
controller = true
`
	return s
}

func richBoard(rng *rand.Rand) restartCase {
	steps := 3 + rng.Intn(3)
	step := func(i int) string { return fmt.Sprintf("fmn_s%d", i) }
	ports := make([][]string, steps)
	for i := range steps {
		ports[i] = []string{"port_in"}
		if i >= 2 && rng.Intn(3) == 0 {
			ports[i] = append(ports[i], "port_b")
		}
	}
	var body, wires strings.Builder
	edge := 0
	var edges [][2]string
	wire := func(from, to string) {
		edge++
		edges = append(edges, [2]string{from, to})
		fmt.Fprintf(&wires, "[[connection]]\nid = \"edge_%d\"\nfrom = \"%s\"\nto = \"%s\"\n", edge, from, to)
	}
	randPort := func(s int) string { return step(s) + ":" + ports[s][rng.Intn(len(ports[s]))] }
	later := func(from int) (int, bool) {
		if from+1 >= steps {
			return 0, false
		}
		return from + 1 + rng.Intn(steps-from-1), true
	}
	fedPort := map[string]bool{}
	tc := restartCase{verdicts: map[string][]string{}, answerWhile: map[string][]string{}, judges: map[string][]string{}}
	wire("mis_proof:out", step(0)+":port_in")
	fedPort[step(0)+":port_in"] = true
	nGates := 1 + rng.Intn(3)
	if nGates > steps {
		nGates = steps
	}
	judged := rng.Perm(steps)[:nGates]
	gateOf := map[int]string{}
	var humanGates []string
	var judgeIDs []string
	for g, s := range judged {
		if rng.Intn(3) == 0 {
			id := fmt.Sprintf("gate_j%d", g)
			judge := fmt.Sprintf("fmn_judge%d", g)
			body.WriteString(`
[[gate]]
id = "` + id + `"
title = "J` + strconv.Itoa(g) + `"
kinds = ["formation"]
criterion = "Judge the work"
`)
			body.WriteString(richFormation(judge, []string{"port_in"}))
			wire(id+":judge", judge+":port_in")
			wire(judge+":port_out", id+":judge")
			var script []string
			for range rng.Intn(3) {
				script = append(script, "fail")
			}
			tc.judges[judge] = append(script, "pass")
			judgeIDs = append(judgeIDs, judge)
			gateOf[s] = id
		} else {
			id := fmt.Sprintf("gate_h%d", g)
			body.WriteString(gateBoardHumanGate(id))
			var script []string
			for range rng.Intn(3) {
				script = append(script, "fail")
			}
			tc.verdicts[id] = append(script, "pass")
			humanGates = append(humanGates, id)
			gateOf[s] = id
		}
	}
	for s := range steps {
		body.WriteString(richFormation(step(s), ports[s]))
	}
	body.WriteString(endNodes)
	for s := range steps {
		if g, ok := gateOf[s]; ok {
			wire(step(s)+":port_out", g+":in")
			if next, ok := later(s); ok && rng.Intn(2) == 0 && len(ports[next]) == 1 {
				p := randPort(next)
				wire(g+":pass", p)
				fedPort[p] = true
			} else {
				wire(g+":pass", "end_done:in")
			}
			switch rng.Intn(4) {
			case 0:
				p := randPort(rng.Intn(s + 1))
				wire(g+":fail", p)
			case 1, 2:
				if next, ok := later(s); ok {
					p := randPort(next)
					wire(g+":fail", p)
				} else {
					wire(g+":fail", "end_rejected:in")
				}
			default:
				wire(g+":fail", "end_rejected:in")
			}
			// Sometimes the judged step also fans out to a later step.
			if next, ok := later(s); ok && rng.Intn(4) == 0 {
				p := randPort(next)
				wire(step(s)+":port_out", p)
				fedPort[p] = true
			}
			continue
		}
		targets := 0
		for range 1 + rng.Intn(2) {
			if next, ok := later(s); ok && rng.Intn(3) != 0 {
				p := randPort(next)
				wire(step(s)+":port_out", p)
				fedPort[p] = true
				targets++
			}
		}
		if targets == 0 {
			wire(step(s)+":port_out", "end_done:in")
		}
	}
	for s := range steps {
		for _, p := range ports[s] {
			if !fedPort[step(s)+":"+p] {
				wire("mis_proof:out", step(s)+":"+p)
			}
		}
	}
	tc.board = gateBoard(body.String() + wires.String())
	// A join whose port only a gate's chosen route can reach may starve; such
	// boards are skipped (the engine blocks them as a wiring gap, by design).
	uncond := map[string]bool{"mis_proof": true}
	for changed := true; changed; {
		changed = false
		for s := range steps {
			if uncond[step(s)] {
				continue
			}
			all := true
			for _, port := range ports[s] {
				ok := false
				for _, e := range edges {
					from := strings.SplitN(e[0], ":", 2)[0]
					if e[1] == step(s)+":"+port && uncond[from] && !strings.HasPrefix(from, "gate_") {
						ok = true
					}
				}
				all = all && ok
			}
			if all {
				uncond[step(s)] = true
				changed = true
			}
		}
	}
	for s := range steps {
		if len(ports[s]) > 1 && !uncond[step(s)] {
			tc.board = "invalid"
		}
	}
	all := []string{}
	for s := range steps {
		all = append(all, step(s))
	}
	all = append(all, judgeIDs...)
	for _, g := range humanGates {
		if rng.Intn(4) != 0 {
			who := all[rng.Intn(len(all))]
			tc.answerWhile[who] = append(tc.answerWhile[who], g)
		}
	}
	tc.midStepOptional = true
	tc.invariants = richInvariants
	return tc
}

// richInvariants holds on every ledger: no recorded verdict stays unrouted,
// and once the run ends every feedback or response a gate routed into a step
// reached a later run of that step.
func richInvariants(t *testing.T, board *formations.BoardDocument, events []formations.RunEvent) {
	t.Helper()
	routed := map[int]bool{}
	for _, e := range events {
		if e.Type == formations.RunEventGateVerdict {
			if seq, ok := e.Data["requestedSeq"].(float64); ok && seq > 0 {
				routed[int(seq)] = true
			}
			if seq, ok := e.Data["requestedSeq"].(int); ok && seq > 0 {
				routed[seq] = true
			}
		}
	}
	for _, e := range events {
		if e.Type == formations.RunEventHumanVerdictRecorded {
			seq := 0
			switch v := e.Data["requestedSeq"].(type) {
			case float64:
				seq = int(v)
			case int:
				seq = v
			}
			if !routed[seq] {
				t.Errorf("INVARIANT: verdict at %d for request %d never routed\n%s", e.Seq, seq, fullTrail(events))
			}
		}
	}
	isFormation := map[string]bool{}
	for _, f := range board.Formations {
		isFormation[f.ID] = true
	}
	connTo := map[string]string{}
	for _, c := range board.Connections {
		connTo[c.ID] = c.To
	}
	// Collect, per node_started, every (gateId, gateAttempt, kind) carried.
	type carried struct {
		gate    string
		attempt int
		kind    string
	}
	seen := func(start formations.RunEvent) map[carried]bool {
		out := map[carried]bool{}
		raw, _ := start.Data["inputRefs"].([]any)
		for _, item := range raw {
			fields, _ := item.(map[string]any)
			for fb, _ := fields["feedback"].(map[string]any); fb != nil; fb, _ = fb["earlier"].(map[string]any) {
				a, _ := fb["gateAttempt"].(float64)
				out[carried{fmt.Sprint(fb["gateId"]), int(a), "feedback"}] = true
			}
			for r, _ := fields["response"].(map[string]any); r != nil; r, _ = r["earlier"].(map[string]any) {
				a, _ := r["gateAttempt"].(float64)
				out[carried{fmt.Sprint(r["gateId"]), int(a), "response"}] = true
			}
		}
		return out
	}
	final := false
	for _, e := range events {
		if e.Type == formations.RunEventSucceeded || e.Type == formations.RunEventFailed {
			final = true
		}
	}
	if !final {
		return
	}
	for i, e := range events {
		if e.Type != formations.RunEventGateVerdict {
			continue
		}
		port, _ := e.Data["routePort"].(string)
		kind := "feedback"
		if port == "pass" {
			kind = "response"
			reason, _ := e.Data["reason"].(string)
			if _, human := e.Data["requestedSeq"]; !human || reason == "" {
				continue
			}
			if rs, _ := e.Data["requestedSeq"].(float64); rs == 0 {
				continue
			}
		} else if port != "fail" {
			continue
		}
		edges, _ := e.Data["routedEdges"].([]any)
		for _, edgeAny := range edges {
			to := connTo[fmt.Sprint(edgeAny)]
			node := strings.SplitN(to, ":", 2)[0]
			if !isFormation[node] {
				continue
			}
			want := carried{e.GateID, e.Attempt, kind}
			found := false
			for _, later := range events[i+1:] {
				if later.Type == formations.RunEventNodeStarted && later.NodeID == node && seen(later)[want] {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("INVARIANT: %s %s attempt %d routed to %s (verdict seq %d) never reached a run of it\n%s", kind, e.GateID, e.Attempt, node, e.Seq, fullTrail(events))
			}
		}
	}
}

func TestRichRandomMissionsReachTheSameOutcomeAfterAnyRestart(t *testing.T) {
	want := 16
	if n, err := strconv.Atoi(os.Getenv("ARCHON_ORACLE_MISSIONS")); err == nil && n > 0 {
		want = n
	}
	boards := 0
	for seed := int64(1); boards < want && seed < int64(want)*40; seed++ {
		tc := richBoard(rand.New(rand.NewSource(seed)))
		if !validMission(t, tc.board) {
			continue
		}
		boards++
		tc.name = fmt.Sprintf("seed %d", seed)
		t.Run(tc.name, func(t *testing.T) { checkRestartCuts(t, tc) })
	}
	if boards < want {
		t.Fatalf("only %d valid missions", boards)
	}
}
