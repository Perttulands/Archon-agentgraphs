package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// run list prints one line per run, newest first, without the runs' events;
// --json keeps the daemon's full projection (archon-lmg2).
func TestRunListPrintsOneLinePerRunNewestFirst(t *testing.T) {
	const body = `{"success":true,"data":[` +
		`{"runId":"run_old","missionSlug":"proof","status":"succeeded","beadId":"","startedAt":"2026-10-01T09:00:00.123Z","updatedAt":"2026-10-01T09:05:00Z","events":[{"seq":1,"type":"run_started"}]},` +
		`{"runId":"run_new","missionSlug":"proof","status":"waiting_human","beadId":"archon-lmg2","startedAt":"2026-10-02T08:00:00Z","updatedAt":"2026-10-02T08:01:30.5Z","events":[{"seq":1,"type":"run_started"},{"seq":2,"type":"node_started"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	stdout, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "list")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	want := "run_new\tproof\twaiting_human\tarchon-lmg2\t2026-10-02T08:00:00Z\t2026-10-02T08:01:30Z\n" +
		"run_old\tproof\tsucceeded\t-\t2026-10-01T09:00:00Z\t2026-10-01T09:05:00Z\n"
	if stdout != want {
		t.Fatalf("run list printed:\n%s\nwant:\n%s", stdout, want)
	}
	stdout, _, code = runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "list", "--json")
	if code != 0 || !strings.Contains(stdout, `"events"`) {
		t.Fatalf("run list --json = %d %s", code, stdout)
	}
}
