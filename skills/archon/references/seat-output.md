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
  brief. Write selected evidence there; keep project deliverables, caches and
  build trees in the project workspace. Ordinary runs have a 32 MiB total
  file/folder capture budget and a 4,096-entry cap (files, directories and links).
  The directory is never shared with another attempt.
- A port's kind decides what its payload is:
  - `text` (the default): `text`, or `ref` naming the absolute path of a
    text file the seat wrote, which Archon reads as the routed text. To route
    only a path (including an image or large file), put the absolute path in
    `text` and omit `ref`; use a typed file/folder port to keep the artifact.
  - `file` or `folder`: `ref` is required and is the absolute path of an
    existing file or directory, of any type; `text` is a one-line summary. A
    missing ref, or a path that is not a file (or not a directory), blocks
    routing like a bad text ref.
- When Archon accepts a file or folder output, wherever it lies, it copies it
  to `<attempt>/outputs/<port>/<basename>` (a reflink where the filesystem
  offers one, otherwise an ordinary copy), makes the copy read-only, and routes
  and records the copy's path. In a folder, links inside it stay links and
  links that lead out of it are replaced by what they point at. A later attempt
  that rewrites the same project file, or a step handed the path, therefore
  leaves every earlier version as it was.
- A file or folder that exceeds the remaining capture budget blocks with
  `output_not_kept`; return a narrower deliverable or ask the driving agent to
  explicitly archive the run before resuming. Do not declare an entire working
  tree merely to pass its path: text ports can carry project-owned paths, which
  are references rather than retained snapshots.
- A step downstream of a file or folder port receives the path in its brief as
  `input file: <path>` or `input folder: <path>`. Input paths are kept copies
  and read-only: write new versions in your own attempt directory.
- Free-form answer text outside the block is not routed.
- The sentinel must carry this run's ID; another run's sentinel never completes
  a dispatch.
- A judge also emits exactly one `archon-verdict` block (see Gates in the main
  skill), separate from its `archon-outputs` block.

A missing or invalid output block or text ref gets one correction turn on the
same live seat. The correction prompt names the ports, failure and text-ref
byte cap. Return a fresh complete response, preserving the work and any judge
verdict or pick. A second invalid output blocks routing; `run status` and
`run logs` show the code and message in text mode.

The cockpit's Produced list opens a run's artifacts, and `archon run attempts
<runId> [--node <id>]` lists each attempt with its outputs, the verdicts on
them and its files.
