Archon is a standalone workbench for teams of coding agents. This release ships
the `archon` CLI, `archond` coordinator and Archon UI together for Linux on
x86-64 and ARM64.

- Draw missions with agent formations, review gates and human approvals.
- The reusable unit is now called a mission and its entry node the Input card.
  `archon board` still works as a deprecated alias for one release.
- Run Claude Code and Codex seats through tmux, with durable run history.
- Inspect staffing and live output through the Agents view and floating Peek.
- Install one archive with `install.sh`; the daemon finds its bundled UI.

Existing `formationsd` commands, `.formations` state, API routes and runtime
output contracts remain compatible. The installer preserves runtime state and
keeps older releases on disk. It does not install tmux, model CLIs, Beads or
shared skills.

Download the archive for your machine and `SHA256SUMS`, verify the checksum,
then follow the README's install instructions. Agent execution requires the
chosen model CLIs to be installed and authenticated. Lab mode runs without
models. Archon listeners require a trusted network; the service has no
application authentication.
