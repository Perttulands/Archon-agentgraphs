package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The default is an inventory read. Archive is a separate explicit choice;
// it does not resume a blocked run or recover already expired evidence.
func runStorage(store *formations.Store, client *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run storage", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	archive := fs.Bool("archive", false, "retain this run outside the ordinary evidence budget; resume separately")
	unarchive := fs.Bool("unarchive", false, "return this run to ordinary evidence retention")
	dispose := fs.Bool("dispose", false, "remove final run evidence, preserving compact history")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "archive": true, "unarchive": true, "dispose": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run storage"))
		return 2
	}
	action := ""
	for name, enabled := range map[string]bool{"archive": *archive, "unarchive": *unarchive, "dispose": *dispose} {
		if enabled {
			if action != "" {
				return fail(stderr, errors.New("choose only one of --archive, --unarchive or --dispose"))
			}
			action = name
		}
	}
	id := fs.Arg(0)
	var report *formations.RunStorageReport
	if client != nil {
		method, path := "GET", "/api/runs/"+url.PathEscape(id)+"/storage"
		var body any
		if action != "" {
			method = "POST"
			body = map[string]string{"action": action}
		}
		raw, err := client.raw(method, path, body)
		if err != nil {
			return fail(stderr, err)
		}
		if *jsonOut {
			fmt.Fprintln(stdout, string(raw))
			return 0
		}
		var envelope struct {
			Data struct {
				Storage formations.RunStorageReport `json:"storage"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fail(stderr, err)
		}
		report = &envelope.Data.Storage
	} else {
		if action != "" {
			release, err := holdStateLock(store.Workspace)
			if err != nil {
				return fail(stderr, err)
			}
			defer release()
			switch action {
			case "archive":
				_, err = store.SetRunArchive(id, true)
			case "unarchive":
				_, err = store.SetRunArchive(id, false)
			case "dispose":
				_, err = store.DisposeRunArtifacts(id)
			}
			if err != nil {
				return fail(stderr, err)
			}
		}
		var err error
		report, err = store.RunStorage(id)
		if err != nil {
			return fail(stderr, err)
		}
		if *jsonOut {
			return writeJSON(stdout, report)
		}
	}
	fmt.Fprintf(stdout, "run: %s\npolicy: %s\nstate: %s\narchived: %t\ntrimmed: %t\nbytes: %d\nallocated bytes: %d\nretained bytes: %d\nordinary run budget: %d\nordinary pool budget: %d\n", report.RunID, report.Policy, report.State, report.Archived, report.Trimmed, report.Bytes, report.AllocatedBytes, report.RetainedBytes, report.BudgetBytes, report.PoolBudgetBytes)
	for _, seq := range report.ActiveSeats {
		fmt.Fprintf(stdout, "unresolved seat: #%d\n", seq)
	}
	for _, source := range report.ProtectedBy {
		fmt.Fprintf(stdout, "protected by: %s\n", source)
	}
	if report.LastError != "" {
		fmt.Fprintf(stdout, "last error: %s\n", report.LastError)
	}
	return 0
}
