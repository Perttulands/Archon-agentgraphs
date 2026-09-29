package main

import (
	"strings"
	"testing"
)

func TestFormationJoinOfflineAndRemote(t *testing.T) {
	offline, remote, _ := newAuthoringSides(t)
	for _, side := range []authoringSide{offline, remote} {
		t.Run(side.name, func(t *testing.T) {
			raw := "schema = 1\nid = \"brd_join\"\nslug = \"join\"\nrev = 1\n"
			for _, id := range []string{"a", "b", "sink"} {
				raw += "\n[[formation]]\nid = \"" + id + "\"\ntype = \"solo\"\n[[formation.input]]\nid = \"in\"\n[[formation.output]]\nid = \"out\"\n"
			}
			writeArchonFile(t, side.store.BoardPath("join"), raw)
			if _, stderr, code := side.run("formation", "wire", "join", "a:out", "sink:in", "--join"); code != 0 {
				t.Fatal(stderr)
			}
			before, _ := side.store.ReadBoard("join")
			if _, stderr, code := side.run("formation", "wire", "join", "b:out", "sink:in", "--json"); code == 0 || !strings.Contains(stderr, `"code": "input_occupied"`) || !strings.Contains(stderr, "Input already has a feed") {
				t.Fatalf("occupied: %d %s", code, stderr)
			}
			if _, stderr, code := side.run("formation", "wire", "join", "b:out", "sink:in", "--join", "--json"); code != 0 {
				t.Fatal(stderr)
			}
			after, _ := side.store.ReadBoard("join")
			if after.Rev != before.Rev+1 || len(after.Formations[2].Inputs) != 2 || len(after.Connections) != 2 || after.Connections[1].To == "sink:in" {
				t.Fatalf("join result: %+v", after)
			}
			for _, tc := range []struct{ from, to, code string }{{"a:out", "sink:in", "duplicate_connection"}, {"sink:out", "sink:in", "self_wire"}} {
				if _, stderr, code := side.run("formation", "wire", "join", tc.from, tc.to, "--join", "--json"); code == 0 || !strings.Contains(stderr, `"code": "`+tc.code+`"`) {
					t.Fatalf("%s: %d %s", tc.code, code, stderr)
				}
			}
			_, stderr, _ := side.run("formation", "wire", "-h")
			if !strings.Contains(stderr, "-join") {
				t.Fatalf("join missing from help: %s", stderr)
			}
		})
	}
}
