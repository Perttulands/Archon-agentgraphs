package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestRunStorageRemoteInventoryAndExplicitActions(t *testing.T) {
	var methods, actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runs/run_storage/storage" {
			t.Errorf("path %s", r.URL.Path)
		}
		methods = append(methods, r.Method)
		if r.Method == "POST" {
			var req struct {
				Action string `json:"action"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			actions = append(actions, req.Action)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"data":{"storage":{"runId":"run_storage","policy":"bounded","final":true,"archived":true,"state":"retained","trimmed":false,"bytes":40000000,"allocatedBytes":40001536,"retainedBytes":39000000,"budgetBytes":33554432,"poolBudgetBytes":268435456,"protectedBy":["run_downstream"],"lastError":"retry failed removal"}}}`))
	}))
	defer server.Close()
	for _, action := range []string{"", "--archive", "--unarchive", "--dispose"} {
		args := []string{"run", "storage", "run_storage"}
		if action != "" {
			args = append(args, action)
		}
		var out, stderr bytes.Buffer
		if code := runRemote(server.URL, args, &out, &stderr); code != 0 {
			t.Fatalf("%s: %d %s", action, code, stderr.String())
		}
		for _, want := range []string{"policy: bounded", "archived: true", "bytes: 40000000", "allocated bytes: 40001536", "retained bytes: 39000000", "ordinary run budget: 33554432", "ordinary pool budget: 268435456", "protected by: run_downstream", "last error: retry failed removal"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("missing %q: %s", want, out.String())
			}
		}
	}
	if strings.Join(methods, ",") != "GET,POST,POST,POST" || strings.Join(actions, ",") != "archive,unarchive,dispose" {
		t.Fatalf("requests %v actions %v", methods, actions)
	}
	var out, stderr bytes.Buffer
	if code := runRemote(server.URL, []string{"run", "storage", "run_storage", "--archive", "--dispose"}, &out, &stderr); code == 0 || len(methods) != 4 {
		t.Fatalf("ambiguous action sent a request: %d %v %s", code, methods, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := runRemote(server.URL, []string{"run", "storage", "run_storage", "--json"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"storage"`) {
		t.Fatalf("json: %d %s %s", code, out.String(), stderr.String())
	}
}

func TestRunStorageOfflineMutationHonorsDaemonOwnership(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	started := startArchonLedgerRun(t, store, "storage", "mission_storage", "input_storage")
	release, err := holdStateLock(store.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"--archive", "--unarchive", "--dispose"} {
		var out, stderr bytes.Buffer
		if code := runStorage(store, nil, []string{started.RunID, action}, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "a daemon owns this state directory") {
			t.Fatalf("%s: %d %s", action, code, stderr.String())
		}
	}
	// Inventory remains a read even while the daemon owns the writer lock.
	var out, stderr bytes.Buffer
	if code := runStorage(store, nil, []string{started.RunID, "--json"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"policy": "bounded"`) {
		t.Fatalf("inventory: %d %s %s", code, out.String(), stderr.String())
	}
	release()
	out.Reset()
	stderr.Reset()
	if code := runStorage(store, nil, []string{started.RunID, "--archive", "--json"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"archived": true`) {
		t.Fatalf("archive: %d %s %s", code, out.String(), stderr.String())
	}
	events, err := store.ReadRunEvents(started.RunID)
	if err != nil || len(events) != 1 {
		t.Fatalf("archive implicitly executed run: %v %v", events, err)
	}
}
