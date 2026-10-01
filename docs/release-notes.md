Archon is a standalone workbench for teams of coding agents. This release ships
the `archon` CLI, `archond` coordinator and Archon UI together for Linux on
x86-64 and ARM64.

- Draw missions with agent formations, review gates and human approvals.
- The reusable unit is a mission and its entry node the Input card, on every
  surface: CLI, HTTP (`/api/missions`, `/api/runs`), JSON fields, files and
  the cockpit.
- Run Claude Code and Codex seats through tmux, with durable run history.
- Inspect staffing and live output through the Agents view and floating Peek.
- Install one archive with `install.sh`; the daemon finds its bundled UI.

This is a breaking release with no compatibility layer. Old names are gone:
`formationsd`, `archon board`, `/api/formations/*`, `?board=`, the
`--mission`/`--board`/`--agent` flags, `form-*` seat sessions, `CHROTE_*`
variables and the `chrote-*` output fences. State lives in
`<state-dir>/.archon/` with `<slug>.mission.toml` files. The deploy migrates
existing state once, with archond stopped, before the new daemon starts. The
installer keeps runtime state and earlier releases on disk. It does not install tmux, model CLIs, Beads or
shared skills.

Download the archive for your machine and `SHA256SUMS`, verify the checksum,
then follow the README's install instructions. Agent execution requires the
chosen model CLIs to be installed and authenticated. Lab mode runs without
models. Archon listeners require a trusted network; the service has no
application authentication.
