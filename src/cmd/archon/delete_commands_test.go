package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestDeleteCommandsCleanGraphAndPreserveOtherNodes(t *testing.T) {
	offline, remote, _ := newAuthoringSides(t)
	for _, side := range []authoringSide{offline, remote} {
		t.Run(side.name, func(t *testing.T) {
			do := func(args ...string) string {
				t.Helper()
				out, stderr, code := side.run(args...)
				if code != 0 {
					t.Fatalf("%v: code %d stderr %s", args, code, stderr)
				}
				return out
			}
			do("mission", "new", "delete-demo", "--title", "Delete demo")
			do("mission", "create", "delete-demo")
			do("formation", "create", "delete-demo", "--title", "Work")
			do("formation", "create", "delete-demo", "--title", "Judge")
			do("gate", "create", "delete-demo", "--title", "Review", "--kinds", "formation")
			judgeBoard, err := side.store.ReadBoard("delete-demo")
			if err != nil {
				t.Fatal(err)
			}
			do("gate", "judge", "delete-demo", "Review", "--chain", formationTitled(t, judgeBoard, "Judge").ID)
			do("limit", "create", "delete-demo", "--target", "Work", "--rounds", "2")
			do("limit", "create", "delete-demo", "--target", "input", "--rounds", "10")
			board, err := side.store.ReadBoard("delete-demo")
			if err != nil {
				t.Fatal(err)
			}
			work := formationTitled(t, board, "Work")
			gate := gateTitled(t, board, "Review")
			workLimit := board.Limits[0].ID
			do("mission", "wire", "delete-demo", work.ID+":"+work.Inputs[0].ID)
			do("formation", "wire", "delete-demo", work.ID+":"+work.Outputs[0].ID, gate.ID+":in")
			do("formation", "wire", "delete-demo", gate.ID+":fail", work.ID+":"+work.Inputs[0].ID)
			var deleted formations.FormationDeleteResult
			if err := json.Unmarshal([]byte(do("formation", "delete", "delete-demo", "Work", "--json")), &deleted); err != nil {
				t.Fatal(err)
			}
			if deleted.FormationID != work.ID || len(deleted.Board.Formations) != 1 || len(deleted.Board.Limits) != 1 || deleted.Board.Limits[0].Target != board.Missions[0].ID {
				t.Fatalf("formation delete: %+v", deleted)
			}
			for _, edge := range deleted.Board.Connections {
				if strings.HasPrefix(edge.From, work.ID+":") || strings.HasPrefix(edge.To, work.ID+":") {
					t.Fatalf("stale route: %+v", edge)
				}
			}
			for _, node := range deleted.Layout.Nodes {
				if node.ID == work.ID || node.ID == workLimit {
					t.Fatalf("stale layout node: %+v", node)
				}
			}
			if deleted.Layout.BoardRev != deleted.Board.Rev {
				t.Fatal("layout revision differs from mission")
			}
			var gateDeleted formations.GateDeleteResult
			if err := json.Unmarshal([]byte(do("gate", "delete", "delete-demo", gate.ID, "--json")), &gateDeleted); err != nil {
				t.Fatal(err)
			}
			if gateDeleted.GateID != gate.ID || len(gateDeleted.Board.Gates) != 0 || len(gateDeleted.Board.Connections) != 0 || len(gateDeleted.Board.Formations) != 1 {
				t.Fatalf("gate delete: %+v", gateDeleted)
			}
			var missionDeleted formations.BoardDeletion
			if err := json.Unmarshal([]byte(do("mission", "delete", board.ID, "--json")), &missionDeleted); err != nil {
				t.Fatal(err)
			}
			if missionDeleted.ID != board.ID || missionDeleted.ArchiveID == "" {
				t.Fatalf("mission delete: %+v", missionDeleted)
			}
			if _, err := side.store.ReadBoard("delete-demo"); !errors.Is(err, formations.ErrNotFound) {
				t.Fatalf("mission remains: %v", err)
			}
			if _, err := side.store.ReadLayout("delete-demo"); !errors.Is(err, formations.ErrNotFound) {
				t.Fatalf("layout remains: %v", err)
			}
		})
	}
}
