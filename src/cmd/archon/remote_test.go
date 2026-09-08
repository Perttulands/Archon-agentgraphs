package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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
	if !strings.Contains(received, `"expectedRev":9`) || !strings.Contains(out.String(), "run_proof") {
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
