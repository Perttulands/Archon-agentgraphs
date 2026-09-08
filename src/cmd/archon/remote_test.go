package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteStartUsesBoardRevisionAndNeverFallsBack(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/formations/boards/proof":
			w.Write([]byte(`{"success":true,"timestamp":"test","data":{"board":{"rev":9}}}`))
		case "/api/formations/runs":
			buf := new(bytes.Buffer)
			buf.ReadFrom(r.Body)
			received = buf.String()
			w.WriteHeader(202)
			w.Write([]byte(`{"success":true,"timestamp":"test","data":{"runId":"run_proof"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	var out, stderr bytes.Buffer
	if code := runRemote(server.URL, []string{"mission", "run", "proof", "--mission", "mis_proof", "--json"}, &out, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if !strings.Contains(received, `"maxAttempts":3`) || !strings.Contains(received, `"expectedRev":9`) || !strings.Contains(out.String(), "run_proof") {
		t.Fatalf("request %s output %s", received, out.String())
	}
	server.Close()
	out.Reset()
	stderr.Reset()
	if code := runRemote(server.URL, []string{"run", "status", "run_proof", "--json"}, &out, &stderr); code == 0 || out.Len() != 0 {
		t.Fatalf("unavailable coordinator fell back: code %d output %s", code, out.String())
	}
}

func TestRemoteRejectsNonloopbackAndRedirects(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://127.0.0.1", "http://127.0.0.1/private"} {
		var out, err bytes.Buffer
		if code := runRemote(url, []string{"run", "list"}, &out, &err); code != 2 {
			t.Fatalf("accepted %s", url)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com", 302) }))
	defer server.Close()
	var out, err bytes.Buffer
	if code := runRemote(server.URL, []string{"run", "list"}, &out, &err); code == 0 {
		t.Fatal("redirect accepted")
	}
}

func TestRemoteRunInputsReadFilesAndLongLiteralBriefs(t *testing.T) {
	cwd := t.TempDir()
	brief := strings.Repeat("A concrete task with several words. ", 40)
	file := filepath.Join(cwd, "brief.md")
	if err := os.WriteFile(file, []byte(brief), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":{"board":{"rev":1}}}`)
			return
		}
		var got struct {
			Cwd    string
			Brief  string
			BeadID string
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if got.Cwd != cwd || got.Brief != brief || got.BeadID != "form-proof" {
			t.Errorf("run inputs: %+v", got)
		}
		fmt.Fprint(w, `{"data":{"runId":"run_proof"}}`)
	}))
	defer server.Close()
	for _, input := range []string{brief, file} {
		var out, stderr bytes.Buffer
		if code := runRemote(server.URL, []string{"mission", "run", "proof", "--mission", "mis_proof", "--cwd", cwd, "--brief", input, "--bead", "form-proof"}, &out, &stderr); code != 0 {
			t.Fatalf("%d %s", code, stderr.String())
		}
	}
}
