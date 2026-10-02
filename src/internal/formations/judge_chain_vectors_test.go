package formations

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// judgeChainVector is one case of testdata/judge_chains.json, which the
// cockpit's judgeChain test reads too, so both sides agree (archon-n7u.51).
type judgeChainVector struct {
	Name        string      `json:"name"`
	Formations  []string    `json:"formations"`
	Connections [][2]string `json:"connections"`
	Chain       []string    `json:"chain"`
}

func TestJudgeChainsMatchTheSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/judge_chains.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []judgeChainVector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		board := &BoardDocument{}
		for _, id := range vector.Formations {
			board.Formations = append(board.Formations, FormationNode{ID: id})
		}
		for i, pair := range vector.Connections {
			board.Connections = append(board.Connections, BoardConnection{ID: "edge_" + strings.Repeat("x", i+1), From: pair[0], To: pair[1]})
		}
		got := []string{}
		for _, judge := range judgeChainForGate(board, "gate_g") {
			got = append(got, judge.ID)
		}
		if !reflect.DeepEqual(got, vector.Chain) {
			t.Errorf("%s: chain = %v, want %v", vector.Name, got, vector.Chain)
		}
	}
}
