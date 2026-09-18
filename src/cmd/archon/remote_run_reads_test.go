package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteRunReads(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		path, body string
		text       []string
	}{
		{"seats", []string{"run", "seats", "run_proof"}, "/api/formations/runs/run_proof/seats", `{"success":true,"data":{"runId":"run_proof","available":true,"seats":[{"nodeId":"fmn_work","slotId":"slot_lead","createdSeq":4,"sessionName":"form-run_proof-slot_lead","state":"live","onCall":{"keptSeq":8,"waitingOn":[{"gateId":"gate_review","requestedSeq":9}]}}]}}`, []string{"seat 4 node fmn_work slot slot_lead state live session form-run_proof-slot_lead", "on call", "waiting gate gate_review requested-seq 9"}},
		{"gates", []string{"run", "gates", "run_proof"}, "/api/formations/runs/run_proof", `{"success":true,"data":{"runId":"run_proof","status":"waiting_human","events":[{"seq":9}],"waitingGates":[{"gateId":"gate_review","requestedSeq":9,"askedSeats":[{"nodeId":"fmn_work","slotId":"slot_lead","createdSeq":4,"deliveredSeq":10}],"fallbackReason":"seat gone"}]}}`, []string{"gate gate_review requested-seq 9", "asked node fmn_work slot slot_lead created-seq 4 delivered-seq 10", "fallback: seat gone"}},
		{"request", []string{"gate", "request", "run_proof", "gate_review"}, "/api/formations/runs/run_proof/gates/gate_review/request", `{"success":true,"data":{"request":{"gateId":"gate_review","requestedSeq":9,"criterion":"Accept the plan?","input":{"fromNodeId":"fmn_work","fromPortId":"port_out","text":"The plan.\nSecond line.","truncated":true}}}}`, []string{"gate gate_review requested-seq 9", "criterion: Accept the plan?", "input from fmn_work:port_out", "The plan.\nSecond line.", "[input truncated]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != tt.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			for _, asJSON := range []bool{false, true} {
				args := append([]string{"--server", server.URL}, tt.args...)
				if asJSON {
					args = append(args, "--json")
				}
				out, stderr, code := runArchon(t, &fakeTmux{}, args...)
				if code != 0 {
					t.Fatalf("%d %s", code, stderr)
				}
				if asJSON {
					if !json.Valid([]byte(out)) {
						t.Fatalf("invalid JSON: %s", out)
					}
					if tt.name == "gates" {
						if strings.Contains(out, `"events"`) || !strings.Contains(out, `"askedSeats"`) || !strings.Contains(out, `"requestedSeq":9`) {
							t.Fatalf("pending gate JSON: %s", out)
						}
					} else if out != tt.body {
						t.Fatalf("JSON response changed: %s", out)
					}
				} else {
					for _, want := range tt.text {
						if !strings.Contains(out, want) {
							t.Errorf("missing %q in %s", want, out)
						}
					}
				}
			}
		})
	}
}

func TestRemoteRunReadErrorsAndEmptyResults(t *testing.T) {
	for _, command := range [][]string{{"run", "seats", "run_missing"}, {"run", "gates", "run_missing"}, {"gate", "request", "run_missing", "gate_review"}} {
		for _, status := range []int{404, 409, 503} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"request unavailable"}`)
			}))
			var out, stderr bytes.Buffer
			if code := runRemote(server.URL, command, &out, &stderr); code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), fmt.Sprintf("HTTP %d", status)) {
				t.Errorf("%v: stdout %s stderr %s", command, &out, &stderr)
			}
			server.Close()
		}
	}
	for _, tt := range []struct{ command, body, want string }{
		{"run gates", `{"data":{"runId":"run_proof","waitingGates":[]}}`, "No pending human gates."},
		{"run seats", `{"data":{"available":false,"reason":"lab executor","seats":[]}}`, "Seat terminals unavailable: lab executor\nNo seats."},
	} {
		var out, stderr bytes.Buffer
		if code := printRemoteRunRead(tt.command, []byte(tt.body), false, &out, &stderr); code != 0 || !strings.Contains(out.String(), tt.want) {
			t.Fatalf("%d %s %s", code, &out, &stderr)
		}
	}
	for _, body := range []string{`{`, `{}`, `{"data":null}`, `{"data":{"seats":"broken"}}`} {
		var out, stderr bytes.Buffer
		if code := printRemoteRunRead("run seats", []byte(body), false, &out, &stderr); code == 0 || out.Len() != 0 {
			t.Errorf("accepted %s: %s", body, &out)
		}
	}
}

func TestRemoteLaunchWorkingDirectoryForwarding(t *testing.T) {
	for _, cwdArgs := range [][]string{nil, {"--cwd", ""}, {"--cwd", "/operator/chosen/project"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				fmt.Fprint(w, `{"data":{"board":{"rev":3}}}`)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			want := ""
			if len(cwdArgs) > 0 {
				want = cwdArgs[1]
			}
			if body["cwd"] != want || body["brief"] != "Build the project" {
				t.Errorf("unexpected admission body: %+v", body)
			}
			fmt.Fprint(w, `{"data":{"runId":"run_proof"}}`)
		}))
		args := append([]string{"mission", "run", "proof", "--mission", "mis_proof", "--brief", "Build the project"}, cwdArgs...)
		var out, stderr bytes.Buffer
		if code := runRemote(server.URL, args, &out, &stderr); code != 0 {
			t.Fatalf("%d %s", code, &stderr)
		}
		server.Close()
	}
}

func TestRemoteStatusAndFollowPreserveProjection(t *testing.T) {
	waiting := `{"success":true,"data":{"runId":"run_proof","status":"waiting_human","final":false,"waitingGates":[{"gateId":"gate_review","requestedSeq":9}]}}`
	final := `{"success":true,"data":{"runId":"run_proof","status":"succeeded","final":true,"waitingGates":[]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("unexpected method %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/formations/runs/run_proof":
			fmt.Fprint(w, waiting)
		case "/api/formations/runs/run_proof/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": keepalive\n\nevent: projection\ndata: %s\n\nevent: projection\ndata: %s\n\n", waiting, final)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	for _, command := range []string{"status", "logs", "follow"} {
		var out, stderr bytes.Buffer
		if code := runRemote(server.URL, []string{"run", command, "run_proof", "--json"}, &out, &stderr); code != 0 {
			t.Fatalf("%s: %d %s", command, code, &stderr)
		}
		want := waiting
		if command == "follow" {
			want += "\n" + final + "\n"
		}
		if out.String() != want {
			t.Errorf("%s: %q, want %q", command, out.String(), want)
		}
	}
}
