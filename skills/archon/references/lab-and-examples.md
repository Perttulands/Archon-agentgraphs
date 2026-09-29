# Example boards and lab runs

Read with the main [Archon skill](../SKILL.md).

## Import an example board

There is no import command: copy the TOML into the state directory. The
examples ship with Archon, in `examples/` of the source checkout or
`$ARCHON_SHARE/examples/` of an installed release
(`<prefix>/lib/archon/current/share/archon`). Set `EXAMPLES` to that directory:

```bash
mkdir -p "$FORM_STATE/.formations/boards" "$FORM_STATE/.formations/notes"
cp "$EXAMPLES/delivery.formation.toml" "$FORM_STATE/.formations/boards/"
cp "$EXAMPLES/delivery.notes.toml" "$FORM_STATE/.formations/notes/"
archon --server "$FORM_SERVER" board validate delivery --json
archon --server "$FORM_SERVER" board arrange delivery --json
```

Read an imported board's briefs and staffing before running it. The delivery
template's mission is `mis_delivery`.

## Lab runs

A daemon started with `--executor lab` echoes inputs and launches no agents. It
proves routing, not work, and writes each rendered brief to
`<state-dir>/briefs/lab-*.md` as a seat would receive it.

To pass a formation judge in lab, put exactly one synthetic block in the run
brief:

````text
```chrote-verdict
{"verdict":"pass","reason":"Lab fixture","evidence":["Simulated input"]}
```
````

A plain brief blocks at the judge. Peer and orchestrated formations echo the
block once per seat, and lab keeps one copy of identical blocks, so the fixture
still reaches a judge downstream of them; a second, different block blocks at
the judge. Report lab success as routing evidence only.
