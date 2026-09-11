package coordinator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/Perttulands/chrote-agent-formations/internal/core"
	"github.com/Perttulands/chrote-agent-formations/internal/formations"
	"github.com/Perttulands/chrote-agent-formations/internal/terminal"
)

type Seat struct {
	RunID       string `json:"runId"`
	NodeID      string `json:"nodeId"`
	NodeTitle   string `json:"nodeTitle"`
	SlotID      string `json:"slotId"`
	SlotLabel   string `json:"slotLabel"`
	Harness     string `json:"harness"`
	Controller  bool   `json:"controller"`
	CreatedSeq  int    `json:"createdSeq"`
	SessionName string `json:"sessionName"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Columns     int    `json:"columns,omitempty"`
	Rows        int    `json:"rows,omitempty"`
	TerminalURL string `json:"terminalUrl,omitempty"`
}
type SeatList struct {
	RunID     string `json:"runId"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Seats     []Seat `json:"seats"`
}
type seatRecord struct {
	seat   Seat
	target terminal.Target
	ended  bool
}

// ConfigureTerminals is startup configuration. Lab leaves it unconfigured.
func (c *Coordinator) ConfigureTerminals(socket, tmuxBin string) error {
	observer, err := terminal.New(terminal.Config{Socket: socket, TmuxBin: tmuxBin})
	if err == nil {
		c.terminalObserver = observer
	}
	return err
}

// seatRecords resolves labels against the run's frozen graph and uses only
// durable seat_created identities. A newer attempt replaces a slot's old URL.
func (c *Coordinator) seatRecords(runID string) ([]seatRecord, map[int]bool, error) {
	events, err := c.store.ReadRunEvents(runID)
	if err != nil {
		return nil, nil, err
	}
	board, err := c.store.ReadRunBoard(runID)
	if err != nil {
		return nil, nil, err
	}
	latest := map[[2]string]seatRecord{}
	sequences := map[int]bool{}
	text := func(event formations.RunEvent, key string) string { value, _ := event.Data[key].(string); return value }
	for _, event := range events {
		key := [2]string{event.NodeID, event.SlotID}
		if event.Type == "seat_created" {
			sequences[event.Seq] = true
			for _, node := range board.Formations {
				if node.ID != event.NodeID {
					continue
				}
				for _, slot := range node.Slots {
					if slot.ID != event.SlotID {
						continue
					}
					harness := text(event, "harness")
					if harness == "" {
						harness = slot.Harness
					}
					latest[key] = seatRecord{
						seat:   Seat{RunID: runID, NodeID: node.ID, NodeTitle: node.Title, SlotID: slot.ID, SlotLabel: slot.Label, Harness: harness, Controller: slot.Controller, CreatedSeq: event.Seq, SessionName: text(event, "sessionName")},
						target: terminal.Target{SessionID: text(event, "sessionId"), PaneID: text(event, "paneId"), SocketIdentity: text(event, "socketIdentity")},
					}
				}
			}
		}
		if event.Type == "seat_cleanup" && text(event, "outcome") == "ended" {
			if record, ok := latest[key]; ok && record.seat.SessionName == text(event, "sessionName") {
				record.ended = true
				latest[key] = record
			}
		}
	}
	records := make([]seatRecord, 0, len(latest))
	for _, record := range latest {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].seat.CreatedSeq < records[j].seat.CreatedSeq })
	return records, sequences, nil
}

func (c *Coordinator) projectSeat(ctx context.Context, record seatRecord) Seat {
	seat := record.seat
	switch {
	case record.ended:
		seat.State, seat.Reason = "ended", "runtime ended this seat"
	case c.terminalObserver == nil:
		seat.State, seat.Reason = "unavailable", "terminal viewing is unavailable for this executor"
	default:
		status := c.terminalObserver.Probe(ctx, record.target)
		seat.State, seat.Reason, seat.Columns, seat.Rows = status.State, status.Reason, status.Columns, status.Rows
		if status.State == "live" {
			seat.TerminalURL = fmt.Sprintf("/api/formations/runs/%s/seats/%d/terminal", url.PathEscape(seat.RunID), seat.CreatedSeq)
		}
	}
	return seat
}

func (c *Coordinator) seats(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runId")
	records, _, err := c.seatRecords(runID)
	if err != nil {
		failure(w, err)
		return
	}
	list := SeatList{RunID: runID, Available: c.terminalObserver != nil, Seats: make([]Seat, 0, len(records))}
	if !list.Available {
		list.Reason = "terminal viewing is unavailable for this executor"
	}
	for _, record := range records {
		list.Seats = append(list.Seats, c.projectSeat(r.Context(), record))
	}
	reply(w, 200, list)
}

func (c *Coordinator) viewTerminal(w http.ResponseWriter, r *http.Request) {
	// HTTP shutdown does not close hijacked sockets. Count every observer and
	// close it on the immediate fence before the writer lock can be released.
	if !c.acquire("") {
		core.WriteError(w, 503, "TERMINAL_UNAVAILABLE", "coordinator shutting down")
		return
	}
	defer c.release("")
	seq, err := strconv.Atoi(r.PathValue("createdSeq"))
	if err != nil || seq < 1 {
		core.WriteError(w, 400, "INVALID_SEAT", "positive createdSeq required")
		return
	}
	records, sequences, err := c.seatRecords(r.PathValue("runId"))
	if err != nil {
		failure(w, err)
		return
	}
	var selected *seatRecord
	for i := range records {
		if records[i].seat.CreatedSeq == seq {
			selected = &records[i]
			break
		}
	}
	if selected == nil {
		if sequences[seq] {
			core.WriteError(w, 409, "STALE_SEAT", "a newer seat attempt replaced this terminal")
		} else {
			core.WriteError(w, 404, "SEAT_NOT_FOUND", "no such seat in this run")
		}
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-c.stopping:
			cancel()
		case <-ctx.Done():
		}
	}()
	seat := c.projectSeat(ctx, *selected)
	if seat.State != "live" {
		code := 409
		if seat.State == "unavailable" {
			code = 503
		}
		core.WriteError(w, code, "TERMINAL_UNAVAILABLE", seat.Reason)
		return
	}
	c.terminalObserver.Serve(ctx, w, r, selected.target)
}
