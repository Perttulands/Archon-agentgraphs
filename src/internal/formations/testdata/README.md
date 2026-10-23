# Test fixtures

Fixtures here hold no real session content. Transcripts in `seats/` and `transcripts/`
follow the native Claude Code and Codex formats but hold only the records and
fields their tests read; session, turn, message and tool IDs, prompts, briefs
and replies are made up, while record order, types and token counts follow what
real seats wrote. The pane captures in `panes/` are screens of throwaway seats
running synthetic prompts in `/work`. The ledgers in `attempt-stops/` use
synthetic node IDs and reasons, and `arrange/` keeps only a board's structure:
IDs, titles and wiring, without briefs.

Never commit a real capture (session log, rollout, pane or run ledger). This
repository is public, a capture carries the session IDs, prompts, output, paths
and account details of whoever ran it, and Git history keeps it after removal.
To cover a new harness format, capture it locally, then write a fixture by hand
with only the records the test needs, placeholder IDs such as `fixture-turn-1`
and paths under `/work`.
