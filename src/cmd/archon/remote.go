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
	"time"
	"unicode/utf8"

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
	if args[0]+" "+args[1] == "run wait" {
		return runWaitRemote(client, args[2:], stdout, stderr)
	}
	if args[0]+" "+args[1] == "mission run" || args[0]+" "+args[1] == "formation run" {
		return remoteRunStart(client, args[0], args[2:], stdout, stderr)
	}
	request := client.raw
	fs := flag.NewFlagSet(args[0]+" "+args[1], flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	var responseFile string
	if args[0] == "gate" && (args[1] == "approve" || args[1] == "reject") {
		fs.StringVar(&responseFile, "response-file", "", "local UTF-8 file containing the complete verbatim response")
	}
	mode := fs.String("mode", "reattach", "resume mode")
	var missionFilter string
	if args[0]+" "+args[1] == "run list" {
		fs.StringVar(&missionFilter, "mission", "", "the mission whose runs to list")
	}
	reason := new(string)
	if args[0] == "gate" && (args[1] == "approve" || args[1] == "reject") {
		fs.StringVar(reason, "response", "", "the response: approve delivers it downstream with the gate input, reject sends it back as feedback")
	} else {
		fs.StringVar(reason, "reason", "", "operator reason")
	}
	seq := fs.Int("requested-seq", 0, "exact pending human request sequence")
	relayedBy := fs.String("relayed-by", "", relayedByUsage)
	if err := fs.Parse(reorderFlags(args[2:], map[string]bool{"json": true})); err != nil {
		return 2
	}
	pos := fs.Args()
	path := "/api"
	method := "GET"
	var body any
	switch args[0] + " " + args[1] {
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
		if missionFilter != "" {
			path += "?mission=" + url.QueryEscape(missionFilter)
		}
	case "run status", "run logs", "run gates", "run seats":
		if len(pos) != 1 {
			return remoteUsage(stderr)
		}
		path += "/runs/" + url.PathEscape(pos[0])
		if args[1] == "seats" {
			path += "/seats"
		}
	case "gate request":
		if len(pos) != 2 {
			return remoteUsage(stderr)
		}
		path += "/runs/" + url.PathEscape(pos[0]) + "/gates/" + url.PathEscape(pos[1]) + "/request"
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
		given := givenFlags(fs)
		if given["response-file"] {
			if given["response"] {
				return fail(stderr, errors.New("--response-file cannot be combined with --response"))
			}
			raw, err := os.ReadFile(responseFile)
			if err != nil {
				return fail(stderr, fmt.Errorf("read --response-file: %w", err))
			}
			if !utf8.Valid(raw) {
				return fail(stderr, errors.New("--response-file must contain valid UTF-8"))
			}
			*reason = string(raw)
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
	if args[0]+" "+args[1] == "run gates" || args[0]+" "+args[1] == "run seats" || args[0]+" "+args[1] == "gate request" {
		return printRemoteRunRead(args[0]+" "+args[1], raw, *jsonOut, stdout, stderr)
	}
	fmt.Fprint(stdout, string(raw))
	return 0
}
func remoteUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "use mission, formation, gate and agent authoring and read commands, mission run <mission> or formation run <mission> <formation> [--input name=value]..., run status|logs|follow|wait|seats|gates <run>, gate request <run> <gate>, or gate approve|reject <run> <gate> --requested-seq <n> [--response text | --response-file path] [--relayed-by slot-id]")
	return 2
}

// remoteRunStart starts a mission run or a single step's run through the
// daemon. Both take the same run fields and the mission's inputs.
func remoteRunStart(client *remoteClient, noun string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(noun+" run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	run := registerRunStartFlags(fs)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	usage, positionals := missionRunUsage, 1
	if noun == "formation" {
		usage, positionals = formationRunUsage, 2
	}
	if fs.NArg() != positionals {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	inputs, err := run.inputs.collect()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw, err := client.raw("GET", "/api/missions/"+url.PathEscape(fs.Arg(0)), nil)
	if err != nil {
		return fail(stderr, err)
	}
	var mission struct {
		Data struct {
			Board formations.BoardDocument `json:"mission"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &mission); err != nil {
		return fail(stderr, err)
	}
	board := &mission.Data.Board
	fields := map[string]any{"mission": fs.Arg(0), "expectedRev": board.Rev, "cwd": *run.cwd, "beadId": *run.bead, "inputs": inputs}
	if noun == "formation" {
		formationID, err := resolveFormationSelector(board, fs.Arg(1))
		if err != nil {
			return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
		}
		fields["formationId"] = formationID
	} else {
		inputCard, err := runInputCard(board, fs.Arg(0))
		if err != nil {
			return failJSON(stderr, err, *jsonOut, "run", "")
		}
		fields["inputCardId"] = inputCard
	}
	if len(run.contextPaths) > 0 {
		fields["contextPaths"] = run.contextPaths
	}
	limits := map[string]any{}
	for key, value := range map[string]int{"maxDispatch": *run.maxDispatch, "maxAttempts": *run.maxAttempts, "wallClockSeconds": *run.wallClock} {
		if value != 0 {
			limits[key] = value
		}
	}
	fields["limits"] = limits
	raw, err = client.raw("POST", "/api/runs", fields)
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprint(stdout, string(raw))
	return 0
}
