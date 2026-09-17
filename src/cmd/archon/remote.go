package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// remoteClient talks to one coordinator. It never follows redirects or uses a
// proxy, and never falls back to an offline store.
type remoteClient struct {
	server string
	http   *http.Client
}

func newRemoteClient(server string) (*remoteClient, error) {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || net.ParseIP(u.Hostname()) == nil {
		return nil, errors.New("--server must be an http URL with a literal IP")
	}
	return &remoteClient{server: server, http: &http.Client{
		Transport:     &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("coordinator redirects are not allowed") },
	}}, nil
}

// send performs one JSON request and returns the status, body and ETag.
func (c *remoteClient) send(method, path string, value any, ifMatch string) (int, []byte, string, error) {
	var body io.Reader
	if value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return 0, nil, "", err
		}
		body = bytes.NewReader(raw)
	}
	r, err := http.NewRequest(method, c.server+path, body)
	if err != nil {
		return 0, nil, "", err
	}
	r.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	response, err := c.http.Do(r)
	if err != nil {
		return 0, nil, "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, "", err
	}
	return response.StatusCode, raw, response.Header.Get("ETag"), nil
}

// raw returns the whole response envelope, as runtime commands print it.
func (c *remoteClient) raw(method, path string, value any) ([]byte, error) {
	status, raw, _, err := c.send(method, path, value, "")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		var envelope struct {
			Error struct {
				Findings []formations.BoardFinding `json:"findings"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil && len(envelope.Error.Findings) > 0 {
			return nil, &formations.RunAdmissionError{Findings: envelope.Error.Findings}
		}
		return nil, fmt.Errorf("coordinator HTTP %d: %s", status, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

// An explicit server selects HTTP for the entire command. Failure never falls
// through to an offline engine or a local ledger.
func runRemote(server string, args []string, stdout, stderr io.Writer) int {
	client, err := newRemoteClient(server)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if command, ok := remoteAuthoringCommands[args[0]+" "+args[1]]; ok {
		return command(client, args[2:], stdout, stderr)
	}
	request := client.raw
	fs := flag.NewFlagSet(args[0]+" "+args[1], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Bool("json", false, "write JSON")
	cwd := fs.String("cwd", "", "absolute run working directory")
	brief := fs.String("brief", "", "brief file path or literal text")
	bead := fs.String("bead", "", "run Beads id")
	mode := fs.String("mode", "reattach", "resume mode")
	mission := fs.String("mission", "", "mission id")
	reason := fs.String("reason", "", "operator reason; for gate approve|reject, the response text")
	fs.StringVar(reason, "response", "", "alias of --reason for gate approve|reject")
	seq := fs.Int("requested-seq", 0, "exact pending human request sequence")
	relayedBy := fs.String("relayed-by", "", relayedByUsage)
	maxDispatch := fs.Int("max-dispatch", 3, "maximum dispatch count")
	maxAttempts := fs.Int("max-attempts", 3, "maximum node attempts")
	wall := fs.Int("wall-clock-seconds", 7200, "run wall clock limit")
	if err := fs.Parse(reorderFlags(args[2:], map[string]bool{"json": true})); err != nil {
		return 2
	}
	pos := fs.Args()
	path := "/api/formations"
	method := "GET"
	var body any
	switch args[0] + " " + args[1] {
	case "mission run":
		if len(pos) != 1 || *mission == "" {
			return remoteUsage(stderr)
		}
		briefText := *brief
		if data, err := os.ReadFile(*brief); err == nil {
			briefText = string(data)
		} else if !os.IsNotExist(err) && !errors.Is(err, syscall.ENAMETOOLONG) {
			return fail(stderr, err)
		}
		raw, err := request("GET", path+"/boards/"+url.PathEscape(pos[0]), nil)
		if err != nil {
			return fail(stderr, err)
		}
		var board struct {
			Data struct {
				Board struct {
					Rev int `json:"rev"`
				} `json:"board"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &board); err != nil {
			return fail(stderr, err)
		}
		path += "/runs"
		method = "POST"
		body = map[string]any{"cwd": *cwd, "brief": briefText, "beadId": *bead, "board": pos[0], "missionId": *mission, "expectedRev": board.Data.Board.Rev, "limits": map[string]any{"maxDispatch": *maxDispatch, "maxAttempts": *maxAttempts, "wallClockSeconds": *wall, "redact": false}}
	case "run abort", "run resume":
		if len(pos) != 1 {
			return remoteUsage(stderr)
		}
		path += "/runs/" + url.PathEscape(pos[0]) + "/" + args[1]
		method = "POST"
		if args[1] == "abort" {
			body = map[string]any{"reason": *reason, "requestedBy": "operator:archon"}
		} else {
			body = map[string]any{"reason": *reason, "actor": "operator:archon", "mode": *mode}
		}
	case "run list":
		path += "/runs"
	case "run status", "run logs":
		if len(pos) != 1 {
			return remoteUsage(stderr)
		}
		path += "/runs/" + url.PathEscape(pos[0])
	case "run follow":
		if len(pos) != 1 {
			return remoteUsage(stderr)
		}
		response, err := client.http.Get(server + path + "/runs/" + url.PathEscape(pos[0]) + "/stream")
		if err != nil {
			return fail(stderr, err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fail(stderr, fmt.Errorf("coordinator HTTP %d", response.StatusCode))
		}
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 65536), 16<<20)
		for scanner.Scan() {
			if raw, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				fmt.Fprintln(stdout, raw)
			}
		}
		if err := scanner.Err(); err != nil {
			return fail(stderr, err)
		}
		return 0
	case "gate approve", "gate reject":
		if len(pos) != 2 || *seq <= 0 {
			return remoteUsage(stderr)
		}
		verdict := "pass"
		if args[1] == "reject" {
			verdict = "fail"
		}
		path += "/runs/" + url.PathEscape(pos[0]) + "/gates/" + url.PathEscape(pos[1]) + "/verdict"
		method = "POST"
		fields := map[string]any{"requestedSeq": *seq, "verdict": verdict, "reason": *reason}
		if *relayedBy != "" {
			fields["relayedBy"] = *relayedBy
		}
		body = fields
	default:
		fmt.Fprintln(stderr, "command unavailable through the standalone coordinator")
		return 2
	}
	raw, err := request(method, path, body)
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprint(stdout, string(raw))
	return 0
}
func remoteUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "use board, mission, formation, gate and agent authoring and read commands, mission run <board> --mission <id>, run status|logs|follow <run>, or gate approve|reject <run> <gate> --requested-seq <n> [--response text] [--relayed-by slot-id]")
	return 2
}
