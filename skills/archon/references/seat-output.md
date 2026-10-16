# What a seat emits

Read with the main [Archon skill](../SKILL.md). The runtime puts this contract
in every seat's brief; this page is for checking why output did not route, or
for an agent working inside a seat.

A real seat finishes its turn with one fenced `archon-outputs` block naming all
and only its declared output port IDs, then this run's exact sentinel:

````text
```archon-outputs
{"port_out":{"text":"Short result"}}
```
<<<ARCHON-DONE run-id=<run-id> status=ok artifact=<path-or-ref>>>
````

- Every dispatch, judges included, gets its own attempt directory,
  `<state-dir>/.archon/artifacts/<runId>/<nodeId>/<attempt>/`, named in its
  brief. Write what the attempt produces there; it is never shared with an
  earlier or later attempt.
- A port's kind decides what its payload is:
  - `text` (the default): `text`, or `ref` naming the absolute path of a
    text file the seat wrote, which Archon reads as the routed text. A missing,
    unreadable, non-text or oversized ref blocks routing.
  - `file` or `folder`: `ref` is required and is the absolute path of an
    existing file or directory, of any type; `text` is a one-line summary. A
    missing ref, or a path that is not a file (or not a directory), blocks
    routing like a bad text ref.
- When Archon accepts a file or folder output that lies outside the attempt
  directory, it copies it to `<attempt>/outputs/<port>/<basename>` (a reflink
  where the filesystem offers one, otherwise an ordinary copy) and routes and
  records the copy's path. A later attempt that rewrites the same project file
  therefore leaves every earlier version readable. A path already inside the
  attempt directory is routed as it is.
- A step downstream of a file or folder port receives the path in its brief as
  `input file: <path>` or `input folder: <path>`.
- Free-form answer text outside the block is not routed.
- The sentinel must carry this run's ID; another run's sentinel never completes
  a dispatch.
- A judge also emits exactly one `archon-verdict` block (see Gates in the main
  skill), separate from its `archon-outputs` block.

The cockpit's Produced list opens a run's artifacts, and `archon run attempts
<runId> [--node <id>]` lists each attempt with its outputs, the verdicts on
them and its files.
