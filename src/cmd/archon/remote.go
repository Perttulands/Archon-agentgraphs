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
	"strings"
	"time"
)

// An explicit server selects HTTP for the entire command. Failure never falls
// through to an offline engine or a local ledger.
func runRemote(server string, args []string, stdout, stderr io.Writer) int {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || net.ParseIP(u.Hostname()) == nil {
		fmt.Fprintln(stderr, "--server must be an http URL with a literal IP")
		return 2
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("coordinator redirects are not allowed") }}
	request := func(method, path string, value any) ([]byte, error) {
		var body io.Reader
		if value != nil {
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(raw)
		}
		r, err := http.NewRequest(method, server+path, body)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		response, err := client.Do(r)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil {
			return nil, err
		}
		if response.StatusCode >= 300 {
			return nil, fmt.Errorf("coordinator HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
		}
		return raw, nil
	}
	fs := flag.NewFlagSet(args[0]+" "+args[1], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Bool("json", false, "write JSON")
	mission := fs.String("mission", "", "mission id")
	reason := fs.String("reason", "", "operator verdict reason")
	seq := fs.Int("requested-seq", 0, "exact pending human request sequence")
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
	case "board list":
		path += "/boards"
	case "board inspect":
		if len(pos) != 1 {
			return remoteUsage(stderr)
		}
		path += "/boards/" + url.PathEscape(pos[0])
	case "mission run":
		if len(pos) != 1 || *mission == "" {
			return remoteUsage(stderr)
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
		body = map[string]any{"board": pos[0], "missionId": *mission, "expectedRev": board.Data.Board.Rev, "limits": map[string]any{"maxDispatch": *maxDispatch, "maxAttempts": *maxAttempts, "wallClockSeconds": *wall, "redact": false}}
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
		response, err := client.Get(server + path + "/runs/" + url.PathEscape(pos[0]) + "/stream")
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
		body = map[string]any{"requestedSeq": *seq, "verdict": verdict, "reason": *reason}
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
	fmt.Fprintln(stderr, "use mission run <board> --mission <id>, run status|logs|follow <run>, or gate approve|reject <run> <gate> --requested-seq <n>")
	return 2
}
