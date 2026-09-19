package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRemoteMissionContextPaths(t *testing.T) {
	for _, paths := range [][]string{nil, {"/context/prior art", "relative/ääni.md", "/context/prior art", ""}} {
		t.Run(fmt.Sprint(paths), func(t *testing.T) {
			starts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/api/formations/boards/proof" {
					fmt.Fprint(w, `{"data":{"board":{"rev":3}}}`)
					return
				}
				if r.Method != "POST" || r.URL.Path != "/api/formations/runs" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				starts++
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				raw, present := body["contextPaths"]
				if paths == nil {
					if present {
						t.Errorf("omitted context changed admission: %s", raw)
					}
				} else {
					var got []string
					if err := json.Unmarshal(raw, &got); err != nil {
						t.Error(err)
					}
					if !reflect.DeepEqual(got, paths) {
						t.Errorf("context = %#v, want %#v", got, paths)
					}
				}
				if string(body["cwd"]) != `""` || string(body["expectedRev"]) != "3" {
					t.Errorf("admission changed: %s", body)
				}
				fmt.Fprint(w, `{"data":{"runId":"run_proof"}}`)
			}))
			defer server.Close()
			args := []string{"--server", server.URL, "mission", "run", "proof", "--mission", "mis_proof", "--brief", "Use supplied context", "--json"}
			for i, path := range paths {
				if i%2 == 0 {
					args = append(args, "--context-path", path)
				} else {
					args = append(args, "--context-path="+path)
				}
			}
			_, stderr, code := runArchon(t, &fakeTmux{}, args...)
			if code != 0 || starts != 1 {
				t.Fatalf("code %d, starts %d: %s", code, starts, stderr)
			}
		})
	}
}

func TestRemoteGateResponseFileIsVerbatim(t *testing.T) {
	for _, answer := range []string{" \t\n" + strings.Repeat("Ääni, 日本語, café, 🎬: keep this line.\r\n", 250) + "\n \t\n", ""} {
		path := filepath.Join(t.TempDir(), "operator response.txt")
		if err := os.WriteFile(path, []byte(answer), 0600); err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{"approve", "reject"} {
			t.Run(fmt.Sprintf("%s/%d bytes", command, len(answer)), func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.Method != "POST" || r.URL.Path != "/api/formations/runs/run_proof/gates/gate_review/verdict" {
						t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					}
					var body struct {
						Reason, Verdict, RelayedBy string
						RequestedSeq               int
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					verdict := "pass"
					if command == "reject" {
						verdict = "fail"
					}
					if body.Reason != answer || body.Verdict != verdict || body.RelayedBy != "slot_lead" || body.RequestedSeq != 19 {
						t.Errorf("response or decision identity changed: %+v", body)
					}
					fmt.Fprint(w, `{"data":{"runId":"run_proof"}}`)
				}))
				defer server.Close()
				_, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "gate", command, "run_proof", "gate_review", "--response-file", path, "--requested-seq", "19", "--relayed-by", "slot_lead", "--json")
				if code != 0 || requests != 1 {
					t.Fatalf("code %d requests %d: %s", code, requests, stderr)
				}
			})
		}
	}
}

func TestRemoteGateResponseFileErrorsNeverSend(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(invalid, []byte{0xff, 0xfe}, 0600); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "unreadable.txt")
	if err := os.WriteFile(unreadable, []byte("not readable"), 0000); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		flags   []string
		message string
	}{
		{"missing", []string{"--response-file", filepath.Join(dir, "missing")}, "read --response-file"},
		{"empty path", []string{"--response-file="}, "read --response-file"},
		{"directory", []string{"--response-file", dir}, "read --response-file"},
		{"invalid UTF-8", []string{"--response-file", invalid}, "valid UTF-8"},
		{"response conflict", []string{"--response-file", invalid, "--response", "answer"}, "cannot be combined"},
		{"empty response conflict", []string{"--response=", "--response-file", invalid}, "cannot be combined"},
		{"reason conflict", []string{"--reason", "answer", "--response-file", invalid}, "cannot be combined"},
		{"empty reason conflict", []string{"--response-file", invalid, "--reason="}, "cannot be combined"},
	}
	if os.Geteuid() != 0 {
		tests = append(tests, struct {
			name    string
			flags   []string
			message string
		}{"unreadable", []string{"--response-file", unreadable}, "read --response-file"})
	}
	for _, command := range []string{"approve", "reject"} {
		for _, tt := range tests {
			t.Run(command+"/"+tt.name, func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					t.Errorf("invalid input sent HTTP %s %s", r.Method, r.URL.Path)
				}))
				defer server.Close()
				args := append([]string{"--server", server.URL, "gate", command, "run_proof", "gate_review", "--requested-seq", "19"}, tt.flags...)
				out, stderr, code := runArchon(t, &fakeTmux{}, args...)
				if code == 0 || out != "" || requests != 0 || !strings.Contains(stderr, tt.message) {
					t.Fatalf("code %d requests %d stdout %q stderr %q", code, requests, out, stderr)
				}
			})
		}
	}
}
