package coordinator

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
	"github.com/gorilla/websocket"
)

func TestSeatProjectionPinsRunAttemptAndFrozenLabels(t *testing.T) {
	c, _, root := fixture(t)
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: c.personas})
	if err != nil {
		t.Fatal(err)
	}
	id := started.RunID
	socket := filepath.Join(root, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	pin, err := core.SocketIdentity(socket)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "fake-tmux")
	script := "#!/bin/sh\n[ \"$3\" = display-message ] || exit 2\n[ \"$6\" = '$20' ] || exit 1\nprintf '$20\\t%%30\\t0\\t100\\t30\\ton\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfigureTerminals(socket, bin); err != nil {
		t.Fatal(err)
	}
	addSeat := func(session, pin string) int {
		t.Helper()
		if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: "seat_created", NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "owned-display-name", "sessionId": session, "paneId": "%30", "socketIdentity": pin}}); err != nil {
			t.Fatal(err)
		}
		events, _ := c.store.ReadRunEvents(id)
		return events[len(events)-1].Seq
	}
	list := func() SeatList {
		t.Helper()
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs/"+id+"/seats", nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var response struct {
			Data SeatList `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"$20", "%30", pin, socket} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("private identity leaked: %s", private)
			}
		}
		return response.Data
	}
	first := addSeat("$20", pin)
	seats := list()
	if !seats.Available || len(seats.Seats) != 1 || seats.Seats[0].State != "live" || seats.Seats[0].Columns != 100 || seats.Seats[0].Rows != 31 {
		t.Fatalf("projection %+v", seats)
	}
	oldURL := seats.Seats[0].TerminalURL
	if !seats.Seats[0].Controller || seats.Seats[0].Harness != "openai-codex" || seats.Seats[0].CreatedSeq != first {
		t.Fatal(seats)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(strings.Replace(testBoard, `title = "Work"`, `title = "Changed later"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if list().Seats[0].NodeTitle != "Work" {
		t.Fatal("seat labels came from mutable draft")
	}
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: "seat_cleanup", NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "owned-display-name", "outcome": "ended"}}); err != nil {
		t.Fatal(err)
	}
	if got := list().Seats[0]; got.State != "ended" || got.TerminalURL != "" {
		t.Fatal(got)
	}
	addSeat("$999", pin)
	if got := list().Seats[0]; got.State != "missing" || got.TerminalURL != "" {
		t.Fatal(got)
	}
	server := httptest.NewServer(c.Handler())
	defer server.Close()
	refusal := func(url, reason string) {
		t.Helper()
		dialer := websocket.Dialer{Subprotocols: []string{"tty"}}
		conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+url, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"columns":160,"rows":48}`)); err != nil {
			t.Fatal(err)
		}
		_, frame, err := conn.ReadMessage()
		if err != nil || !strings.Contains(string(frame), reason) {
			t.Fatalf("refusal %q: %v", frame, err)
		}
	}
	refusal(oldURL, "a newer seat attempt replaced this terminal")
	addSeat("$20", "")
	if got := list().Seats[0]; got.State != "unavailable" || got.TerminalURL != "" {
		t.Fatal(got)
	}
	refusal("/api/runs/"+id+"/seats/999/terminal", "no such seat in this run")
	// A different run cannot borrow an event sequence from this run.
	other, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: c.personas})
	if err != nil {
		t.Fatal(err)
	}
	refusal(strings.Replace(oldURL, id, other.RunID, 1), "no such seat in this run")
	c.terminalObserver = nil
	if got := list(); got.Available || got.Reason == "" {
		t.Fatal(got)
	}
}
