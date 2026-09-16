# Run evidence API

Accepted 2026-09-16. Implements the operator's decision in form-3l7 (option B)
through form-3rq. Reaffirms the trust boundary of
[ADR-0016](0016-daily-capability.md).

The cockpit could not show what a run produced. Run projections and SSE carry
identities, statuses and verdicts only, so the node inspector's output, gate
reason and judge evidence stayed empty on the daemon. The content existed in the
private ledger, the run's artifact directory and the briefs written for seats.

Projections and SSE stay sanitized. A separate set of read routes serves a run's
content to the trusted operator. The cockpit is reachable over the tailnet
without application authentication by design, so this adds no trust boundary; it
bounds what one request can read and where from.

## Routes

All routes are `GET`, read one run and return 404 for an unknown run.

| Route | Returns |
| --- | --- |
| `/api/formations/runs/{runId}/evidence/nodes/{nodeId}` | `data.evidence` for one node of the run's frozen board. Missions and formations list attempts: routed inputs, dispatches, the output text and per-port outputs. Gates list evaluations: criterion, input, per-kind results with judge evidence, judge failures, the human request with its verdict and response, and the final verdict with its route. Blocks and errors recorded against the node are included. An unknown node is 404. |
| `/api/formations/runs/{runId}/evidence/briefs/{dispatchSeq}` | `data.brief`, the brief file the dispatch at that ledger sequence sent to its seat. A sequence that is not this run's `slot_dispatch` is 404. |
| `/api/formations/runs/{runId}/evidence/artifacts` | `data.artifacts`, the files in the run's artifact directory by relative name, size and modification time. |
| `/api/formations/runs/{runId}/evidence/artifacts/{name...}` | `data.artifact`, a preview: name, size, kind (`markdown`, `json`, `text`, `image` or `binary`) and text for textual kinds. |
| `/api/formations/runs/{runId}/artifacts/{name...}` | The artifact's bytes, for opening in a browser tab. |
| `/api/formations/runs/{runId}/gates/{gateId}/request` | The pending human request added by form-3yd.4, unchanged. It is the evidence API's view of a request still waiting for an answer; after the verdict, the gate's node evidence holds the same input with the response. |

Every capped text is an object `{text, bytes, truncated}`: `bytes` is the
original size and `truncated` marks a cut. Cuts fall on a UTF-8 rune boundary.

## Caps

- Each text in node evidence: 64 KiB, the gate request's cap. A kind result or
  verdict lists at most 100 evidence items and counts the rest in
  `evidenceOmitted`.
- One node evidence response: 2 MiB of text in total. Texts past that budget
  are returned empty and marked truncated.
- A brief: 256 KiB. An artifact preview: 256 KiB of text; binary files return
  no text.
- The artifact list: 500 entries, 8 directory levels.
- A raw artifact: 16 MiB; larger files return 413 and stay readable on the host.

## Confinement

Reads stay inside the run's own ledger, artifact directory and briefs, using the
openat helpers in `run_artifacts.go` and `authority_guard.go`.

- Artifacts resolve under `<state-dir>/.formations/artifacts/<runId>`. The
  daemon opens every component from the state directory with `O_NOFOLLOW`, so a
  symlink at any level fails. Names must be relative, with no empty, `.` or `..`
  component. Only regular files with one link are read, so a hard link cannot
  reach a file elsewhere. The listing skips symlinks, devices, FIFOs, sockets
  and hard-linked files.
- A brief is read only through the `briefPath` recorded by this run's own
  `slot_dispatch` event, only when that path names a direct child of
  `<state-dir>/briefs`, and it is opened the same way. The briefs directory is
  shared by runs; a brief path is never accepted from the request.
- Output and input references are reported as artifact names relative to the
  run's artifact directory. A reference outside it is flagged `external` with
  its base name only and cannot be read through the API.

## Excluded

Structured fields never carry native session IDs (harness session IDs, tmux
session and pane IDs, `sessionRef`), socket or prompt digests, brief or prompt
paths, seat report pointers or absolute artifact paths. Worker pane captures
are not served. The routes read no environment, configuration or credential
files. Served text, including textual raw artifacts, passes through the
ledger's secret patterns (`sk-` and Slack-style tokens, `api_key`, `token`,
`secret` and `password` assignments). Otherwise text is served as written:
outputs, reasons, briefs and artifacts can mention host paths such as the
run's cwd, and briefs already tell agents not to reference secrets.

The raw artifact route never serves active content. UTF-8 text, including
HTML and SVG source, is sent as `text/plain; charset=utf-8`; PNG, JPEG, GIF and
WebP keep their image types; any other file is sent as an
`application/octet-stream` attachment. Every raw response carries
`X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and
`Cache-Control: no-store`.

## Reused from CHROTE

CHROTE and Archon are both MIT licensed under the same copyright holder. Ported
files name their CHROTE source in a header comment, as the seat terminal port
(form-7n0) did.

- The cockpit renders Markdown with CHROTE's `Markdown.tsx` and `Markdown.css`
  (react-markdown 10 and remark-gfm 4, MIT; about 99 packages and 8 MB in
  `node_modules`). The inspector loads them lazily, so the first paint does
  not grow. Raw HTML stays text and links are limited to http, https and
  mailto, as in CHROTE. Relative links resolve to the run's artifact names
  instead of host paths.
- Logs, code and JSON use CHROTE's `TextLines` line view and `prettyJson` from
  `FilePanelViewer.tsx`.
- Not ported: `FileViewer`, the `FilePanelViewer` shell, `FilePopout`,
  `ImageGlance` and `Peek`. They depend on CHROTE's files API, status context,
  floating frames, dismiss registry or tmux sessions. Archon already has its own
  seat Peek. A full artifact opens through the raw route in a tab, and images
  render from the same route.
- Go file serving is not ported. CHROTE's `files.go` checks a canonical root
  and then walks with openat and `O_NOFOLLOW`. Archon's run artifact helpers
  already do the same walk, and CHROTE's raw route has no size cap or
  content-type hardening.
- Neither project highlights syntax; this decision adds none.

## Consequences

The inspector can show real outputs, gate reasons, judge evidence, human
responses, briefs and artifacts on the deployed daemon, with bounded requests.
Evidence routes read files the projection never touches, so their confinement
tests are part of the contract. Content the operator needs beyond a cap stays on
the host.
