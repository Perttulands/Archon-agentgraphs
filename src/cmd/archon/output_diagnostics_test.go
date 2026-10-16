package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestBlockedOutputDiagnostics(t *testing.T) {
	var logs bytes.Buffer
	writeRunEventsText(&logs, []formations.RunEvent{{Seq: 1, Type: formations.RunEventError, Data: map[string]any{"code": "invalid_output_ref", "message": "port_out image is not text"}}, {Seq: 2, Type: formations.RunEventBlocked, Data: map[string]any{"code": "invalid_output_ref", "reason": "executor_failed"}}})
	if !strings.Contains(logs.String(), "invalid_output_ref\tport_out image is not text") {
		t.Fatal(logs.String())
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/evidence/problems") {
			fmt.Fprint(w, `{"data":{"problems":[{"seq":1,"type":"error","code":"invalid_output_ref","reason":{"text":"port_out image is not text"}},{"seq":2,"type":"run_blocked","code":"invalid_output_ref","reason":{"text":"executor_failed"}}]}}`)
		} else {
			fmt.Fprint(w, `{"data":{"runId":"run_test","status":"blocked","blockCode":"invalid_output_ref"}}`)
		}
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	if code := runRemote(server.URL, []string{"run", "status", "run_test"}, &out, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "blocked\tinvalid_output_ref\tport_out image is not text") {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := runRemote(server.URL, []string{"run", "logs", "run_test"}, &out, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "1\terror\tinvalid_output_ref\tport_out image is not text") {
		t.Fatal(out.String())
	}

}
