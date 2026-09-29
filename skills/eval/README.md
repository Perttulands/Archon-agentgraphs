# Archon skill eval

One command tests whether a fresh agent answers Archon questions correctly from
the [archon skill](../archon/SKILL.md) alone:

```bash
python3 skills/eval/run.py skills/archon --out /tmp/archon-skill-eval
```

It prints a verdict per question, a sandbox check and a total, writes each
agent's stream-json transcript and the critic's verdict under `--out`, and exits
0 only when every question passes and the sandbox held. `--only <id> ...` runs a
subset; `--model` and `--critic-model` choose models (default: the harness
default); `--jobs` sets parallel agents.

## Questions

[archon-questions.toml](archon-questions.toml) holds each question with an
expected answer written from [CONTRACT](../../docs/CONTRACT.md) and the key
points the critic checks. **A contract change that touches authoring, running or
deciding adds a question or updates the affected one, in the same change that
updates the contract and the skill.**

## What the agent under test can see

Each question runs a fresh `claude -p` (not `--bare`, so skill discovery works as
it does for real agents):

- Every call gets its own temporary `HOME` whose only user-level skill is
  `~/.claude/skills/archon`, a link to the skill under test. It starts with no
  user settings, memory, `CLAUDE.md` or MCP servers. The environment is rebuilt
  from scratch so no parent session leaks in.
- Claude Code would otherwise install the account's synced plugins (such as
  `anthropic-skills` or `cowork-plugin-management`) into a new `HOME` and load
  them in the next session there. `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
  stops that sync. Claude Code's bundled skills and `@builtin` plugins still
  load; they carry no Archon knowledge.
- Each result records the session's `init_plugins` and `init_skills`. A question
  fails when any plugin is not `@builtin` or when `archon` is not exactly one
  skill.
- The working directory is an empty scratch directory outside any repository,
  so no project `CLAUDE.md` or `.claude/skills` is found.
- `--tools Skill,Read,Glob,Grep --permission-mode dontAsk` with read rules for
  the skill directory only: every other file access is refused.
- A canary run asks an agent to read a file outside the skill. The command fails
  unless that read of the canary path is refused and the contents stay out of
  the transcript. Each result lists its tool calls, counts refusals and flags
  any unrefused read outside the skill.

`python3 skills/eval/selftest.py` checks the harness itself. It runs
[selftest-questions.toml](selftest-questions.toml), whose one question makes the
agent try a read outside the skill. It passes when `results.json` shows
`refused > 0`, no unrefused outside read, only builtin plugins and a held
sandbox, and `run.py` exits 0.

A second fresh `claude -p` with no tools grades each answer against the expected
answer and key points, returning a schema-checked `{verdict, reason, missing}`.

## Auth

The command uses the host's Claude auth: `ANTHROPIC_API_KEY` or
`CLAUDE_CODE_OAUTH_TOKEN` when set, otherwise it links
`~/.claude/.credentials.json` into the temporary `HOME`. No secret is stored in
the repository or the output directory.

## CI

The eval calls a model, so it runs in its own workflow rather than in `ci.yml`,
triggered by hand or when `skills/` or `docs/CONTRACT.md` change. It would
install Claude Code, take a token from a repository secret and upload the
transcripts:

```yaml
name: Skill eval
on:
  workflow_dispatch:
  pull_request:
    paths: [skills/**, docs/CONTRACT.md]
jobs:
  eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: {node-version: 22}
      - run: npm install -g @anthropic-ai/claude-code
      - run: python3 skills/eval/run.py skills/archon --out "$RUNNER_TEMP/skill-eval"
        env:
          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
      - if: always()
        uses: actions/upload-artifact@v4
        with: {name: skill-eval, path: "${{ runner.temp }}/skill-eval"}
```

This workflow is not enabled; adding it and its secret is the owner's decision.
