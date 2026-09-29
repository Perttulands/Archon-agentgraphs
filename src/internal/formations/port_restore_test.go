package formations

import (
	"errors"
	"reflect"
	"testing"
)

func TestRestoreFormationPortPutsBackARemovedPortInPlace(t *testing.T) {
	for _, tc := range []struct {
		portID, direction string
		index             int
	}{
		{"port_build_in", FormationPortInput, 0},
		{"port_build_rework", FormationPortInput, 1},
		{"port_build_out", FormationPortOutput, 0},
		{"port_build_log", FormationPortOutput, 1},
	} {
		t.Run(tc.portID, func(t *testing.T) {
			store := nodeRestoreFixture(t)
			before, _ := store.ReadBoard("restore")
			build, _ := findFormation(before.Formations, "fmn_build")
			var port FormationPort
			for _, candidate := range append(append([]FormationPort(nil), build.Inputs...), build.Outputs...) {
				if candidate.ID == tc.portID {
					port = candidate
				}
			}
			endpoint := "fmn_build:" + tc.portID
			var wires []BoardConnection
			for _, connection := range before.Connections {
				if connection.From == endpoint || connection.To == endpoint {
					wires = append(wires, connection)
				}
			}
			if _, err := store.RemoveFormationPort("restore", FormationPortRemovalRequest{FormationID: "fmn_build", PortID: tc.portID}, restoreOptions(t, store)); err != nil {
				t.Fatal(err)
			}
			req := PortRestoreRequest{FormationID: "fmn_build", Direction: tc.direction, Port: port, Index: tc.index, Connections: wires}
			restored, err := store.RestoreFormationPort("restore", req, restoreOptions(t, store))
			if err != nil {
				t.Fatalf("restore port: %v", err)
			}
			if restored.Rev != before.Rev+2 {
				t.Fatalf("rev = %d, want %d", restored.Rev, before.Rev+2)
			}
			got, _ := findFormation(restored.Formations, "fmn_build")
			if !reflect.DeepEqual(got, build) {
				t.Fatalf("formation = %+v\nwant %+v", got, build)
			}
			if !reflect.DeepEqual(touching(restored, "fmn_build"), touching(before, "fmn_build")) {
				t.Fatalf("connections = %+v\nwant %+v", touching(restored, "fmn_build"), touching(before, "fmn_build"))
			}
			if _, err := store.RestoreFormationPort("restore", req, restoreOptions(t, store)); !errors.Is(err, ErrInvalidNodeRestore) {
				t.Fatalf("second restore err = %v, want invalid restore", err)
			}
		})
	}
	store := nodeRestoreFixture(t)
	for name, req := range map[string]PortRestoreRequest{
		"direction":    {FormationID: "fmn_build", Direction: "in", Port: FormationPort{ID: "port_x"}},
		"missing node": {FormationID: "fmn_gone", Direction: FormationPortInput, Port: FormationPort{ID: "port_x"}},
		"foreign wire": {FormationID: "fmn_build", Direction: FormationPortInput, Port: FormationPort{ID: "port_x"},
			Connections: []BoardConnection{{ID: "edge_x", From: "gate_tests:pass", To: "fmn_judge:port_judge_in"}}},
	} {
		before, _ := store.ReadBoard("restore")
		if _, err := store.RestoreFormationPort("restore", req, restoreOptions(t, store)); err == nil {
			t.Fatalf("%s: restore was accepted", name)
		}
		if after, _ := store.ReadBoard("restore"); after.ETag != before.ETag {
			t.Fatalf("%s: a refused restore changed the board", name)
		}
	}
}
