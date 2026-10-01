# Example missions and lab runs

Read with the main [Archon skill](../SKILL.md).

## Import an example mission

There is no import command: copy the TOML into the state directory. The
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
