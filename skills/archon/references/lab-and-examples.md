# Lab daemon, example missions and notifications

Read with the main [Archon skill](../SKILL.md).

## Build and launch

Use Go, Node/npm, Bash, curl and jq. Take the server URL, listen address and
source, binary, UI, state, socket and transcript paths from the host runbook or
environment; host forwarding reaches the cockpit over the tailnet. Set
`ARCHON_SOURCE`, `ARCHON_BIN`, `ARCHON_STATE`, `ARCHON_LISTEN` and `ARCHON_SERVER`
accordingly. Keep runtime state outside the source checkout. For configuration
rules, read [Daemon configuration](../../../docs/CONTRACT.md#daemon-configuration)
in `docs/CONTRACT.md` in the Archon source or
`$ARCHON_SHARE/docs/CONTRACT.md` in an installed release.

Build and launch a scratch lab daemon, leaving its terminal open:

```bash
umask 077
mkdir -p "$ARCHON_BIN"
cd "$ARCHON_SOURCE/src"
go build -o "$ARCHON_BIN/archon" ./cmd/archon
go build -o "$ARCHON_BIN/archond" ./cmd/archond
cd "$ARCHON_SOURCE/dashboard"
npm ci
npm run build
"$ARCHON_BIN/archond" --executor lab --state-dir "$ARCHON_STATE" \
  --listen "$ARCHON_LISTEN" --ui-dir "$ARCHON_SOURCE/dashboard/dist"
```

In another terminal, select the compiled CLI and check the running build:

```bash
export PATH="$ARCHON_BIN:$PATH"
curl --noproxy '*' --fail --silent --show-error "$ARCHON_SERVER/healthz"
```

## Import an example mission

There is no import command: copy the TOML into the state directory, or
symlink a mission kept in a repository so edits write through to it. If the
repository file moves, `mission list` shows the link as broken and names its
target; the other missions keep working. The
examples ship with Archon, in `examples/` of the source checkout or
`$ARCHON_SHARE/examples/` of an installed release
(`<prefix>/lib/archon/current/share/archon`). Set `EXAMPLES` to that directory:

```bash
mkdir -p "$ARCHON_STATE/.archon/missions" "$ARCHON_STATE/.archon/notes"
cp "$EXAMPLES/delivery.mission.toml" "$ARCHON_STATE/.archon/missions/"
cp "$EXAMPLES/delivery.notes.toml" "$ARCHON_STATE/.archon/notes/"
archon --server "$ARCHON_SERVER" mission validate delivery --json
archon --server "$ARCHON_SERVER" mission arrange delivery --json
```

For offline authoring, use `--workspace "$ARCHON_STATE"` instead of `--server`
against a state directory no daemon serves. Inspect the imported files before
validation with `mission list --json`, `mission inspect delivery --json` and
`mission notes delivery --json`.

Read an imported mission's briefs and staffing before running it. The delivery
template's Input card is `mis_delivery`, and a run supplies its one input,
`change`, with `--input change=...` or `--input-file change=<file>`; the target
repository is the run's `--cwd` and the owning Bead its `--bead`.

## Lab runs

A daemon started with `--executor lab` echoes inputs and launches no agents. It
proves routing, not work, and writes each rendered brief to
`<state-dir>/briefs/lab-*.md` as a seat would receive it.

To pass a formation judge in lab, put exactly one synthetic block in an input
the first step receives (for example `--input-file change=<file>` for Delivery):

````text
```archon-verdict
{"verdict":"pass","reason":"Lab fixture","evidence":["Simulated input"]}
```
````

A plain brief blocks at the judge. Peer and orchestrated formations echo the
block once per seat, and lab keeps one copy of identical blocks, so the fixture
still reaches a judge downstream of them; a second, different block blocks at
the judge. Report lab success as routing evidence only.

To smoke-test a best card in lab, give the input a failing `archon-verdict`
block and an `archon-pick` block naming a judged attempt; the judges fail each
round until the card is spent, then pick:

````text
```archon-verdict
{"verdict":"fail","reason":"Lab fixture","evidence":[]}
```
```archon-pick
{"attempt":2,"reason":"Lab fixture","evidence":[]}
```
````

To run Delivery, set `ARCHON_CHANGE` to the change (including the synthetic
verdict for a lab run) and `ARCHON_BEAD` to the owning Bead:

```bash
ARCHON_START=$(archon --server "$ARCHON_SERVER" mission run delivery \
  --input change="$ARCHON_CHANGE" --bead "$ARCHON_BEAD" --json)
ARCHON_RUN_ID=$(printf '%s\n' "$ARCHON_START" | jq -er '.data.runId')
```

To work in an existing project, set `ARCHON_CWD` to its absolute path and add
`--cwd "$ARCHON_CWD"`. Add a Limit card if the mission needs a loop cap. Follow
the run with [Watch](../SKILL.md#watch).

## Configure notifications

Read [Needs-you notifications](../../../docs/CONTRACT.md#needs-you-notifications)
for the notify-command contract. Supply `--notify-command` with the absolute
path to a host script, and `--cockpit-url` with the cockpit base URL.
A placeholder script reads the JSON and hands the message to a host sender:

```sh
#!/bin/sh
set -eu
notification=$(cat)
subject=$(printf '%s' "$notification" | jq -r '.subject')
printf '%s' "$notification" | jq -r '.body' |
  "$NOTIFY_SENDER" --to "$NOTIFY_TO" --subject "$subject"
```

`NOTIFY_SENDER` and `NOTIFY_TO` stand for host configuration. Keep the real
script, sender and address with the host deployment.
