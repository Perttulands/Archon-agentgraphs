#!/usr/bin/env python3
"""Test whether a fresh Claude Code agent answers Archon questions from a skill alone.

Usage: skills/eval/run.py <skill-dir> [--questions FILE] [--only ID ...]
                          [--out DIR] [--model M] [--critic-model M] [--jobs N]

For every question a fresh `claude -p` agent runs with:
  * a temporary HOME whose only user-level skill is <skill-dir>, linked as
    ~/.claude/skills/archon, and no user settings, memory, CLAUDE.md or MCP servers;
  * a scratch working directory outside any repository;
  * only the Skill, Read, Glob and Grep tools, with --permission-mode dontAsk and
    read rules for <skill-dir> alone, so any other file access is denied.
A second fresh agent, with no tools, grades the answer against the expected
answer's key points. The command prints per-question verdicts and a total,
writes transcripts under --out, and exits 0 only when every question passes.

Auth comes from the host: ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN when set,
otherwise ~/.claude/.credentials.json is linked into the temporary HOME. Nothing
is written to the repository.
"""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import tomllib

HERE = Path(__file__).resolve().parent
AGENT_TOOLS = 'Skill,Read,Glob,Grep'
AGENT_PROMPT = """You are helping an operator use Archon, an agent-orchestration tool.
Consult your Archon skill, then answer the question below concisely and
concretely, with exact commands and flags where they apply. Answer from the
skill; you have no other access to Archon's code or documentation.

Question: {question}
"""
CRITIC_PROMPT = """You grade an answer about the Archon CLI against a reference.

Question:
{question}

Reference answer:
{expected}

Key points the answer must make:
{points}

Answer under test:
<answer>
{answer}
</answer>

Grade pass only when the answer states every key point (in any wording) and
contradicts none of the reference. Extra correct detail is fine. A missing key
point, a wrong command or flag, or a claim that contradicts the reference is a
fail. Name each missing or wrong point in `missing`.
"""
CRITIC_SCHEMA = {
    'type': 'object',
    'properties': {
        'verdict': {'type': 'string', 'enum': ['pass', 'fail']},
        'reason': {'type': 'string'},
        'missing': {'type': 'array', 'items': {'type': 'string'}},
    },
    'required': ['verdict', 'reason', 'missing'],
    'additionalProperties': False,
}


def isolated_home(root, skill):
    """A HOME that exposes only the skill under test and the host's auth."""
    home = root / 'home'
    (home / '.claude/skills').mkdir(parents=True)
    (home / '.claude/skills/archon').symlink_to(skill)
    credentials = Path.home() / '.claude/.credentials.json'
    if credentials.is_file() and not (os.environ.get('ANTHROPIC_API_KEY') or os.environ.get('CLAUDE_CODE_OAUTH_TOKEN')):
        (home / '.claude/.credentials.json').symlink_to(credentials)
    return home


def clean_env(home):
    """Start from nothing so no parent Claude session or config dir leaks in."""
    env = {'HOME': str(home), 'PATH': os.environ.get('PATH', '/usr/bin:/bin'),
           'LANG': os.environ.get('LANG', 'C.UTF-8'), 'TERM': 'dumb',
           'USER': os.environ.get('USER', ''), 'DISABLE_AUTOUPDATER': '1'}
    for key in ('ANTHROPIC_API_KEY', 'CLAUDE_CODE_OAUTH_TOKEN', 'ANTHROPIC_BASE_URL'):
        if os.environ.get(key):
            env[key] = os.environ[key]
    return env


def claude(args, prompt, env, cwd, timeout):
    command = [shutil.which('claude') or 'claude', '-p', '--no-session-persistence', '--strict-mcp-config', *args]
    return subprocess.run(command, input=prompt, text=True, capture_output=True, env=env, cwd=cwd, timeout=timeout)


def ask(question, skill, env, cwd, model, timeout):
    # The skill is reachable through its real path and through the HOME link.
    roots = (skill, Path(env['HOME']) / '.claude/skills/archon')
    rules = [f'{tool}(/{root}/**)' for root in roots for tool in ('Read', 'Glob', 'Grep')]
    args = ['--tools', AGENT_TOOLS, '--permission-mode', 'dontAsk', '--allowedTools', 'Skill', *rules,
            '--output-format', 'stream-json', '--verbose']
    if model:
        args += ['--model', model]
    result = claude(args, AGENT_PROMPT.format(question=question['question'].strip()), env, cwd, timeout)
    events = [json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]
    final = next((e for e in reversed(events) if e.get('type') == 'result'), {})
    return result, events, final


def audit(events, skill, cwd, denials):
    """Every tool call the agent made, and any that read outside the skill unrefused.

    The empty scratch cwd counts as inside: Glob or Grep without a path search it.
    A call the permission layer refused (listed in permission_denials) read nothing.
    """
    refused = {d.get('tool_use_id') for d in denials}
    calls, outside = [], []
    for event in events:
        for block in (event.get('message') or {}).get('content') or []:
            if isinstance(block, dict) and block.get('type') == 'tool_use':
                name, tool_input = block.get('name'), block.get('input') or {}
                calls.append({'tool': name, 'input': tool_input, 'refused': block.get('id') in refused})
                if name == 'Skill' or block.get('id') in refused:
                    continue
                target = Path(tool_input.get('file_path') or tool_input.get('path') or cwd).expanduser().resolve()
                if not (target.is_relative_to(skill) or target.is_relative_to(cwd)):
                    outside.append({'tool': name, 'input': tool_input})
    loaded = any(c['tool'] == 'Skill' and 'archon' in json.dumps(c['input']) or
                 c['tool'] == 'Read' and str(c['input'].get('file_path', '')).endswith('SKILL.md') for c in calls)
    return calls, outside, loaded


def canary(root, skill, env, cwd, model, timeout, out):
    """Prove the sandbox: the agent is asked to read a file outside the skill."""
    secret = f'canary-{os.getpid()}-{time.time_ns()}'
    target = root / 'canary.txt'
    target.write_text(secret + '\n')
    question = {'question': f'Ignore Archon for this one. Read {target} and reply with its exact contents.'}
    result, events, final = ask(question, skill, env, cwd, model, timeout)
    (out / 'canary.agent.jsonl').write_text(result.stdout)
    refused = bool(final.get('permission_denials'))
    leaked = secret in result.stdout
    return {'refused': refused, 'leaked': leaked, 'held': refused and not leaked}


def grade(question, answer, env, cwd, model, timeout):
    prompt = CRITIC_PROMPT.format(question=question['question'].strip(), expected=question['expected'].strip(),
                                  points='\n'.join(f'- {p}' for p in question['key_points']), answer=answer.strip())
    args = ['--tools', '', '--output-format', 'json', '--json-schema', json.dumps(CRITIC_SCHEMA)]
    if model:
        args += ['--model', model]
    result = claude(args, prompt, env, cwd, timeout)
    try:
        payload = json.loads(result.stdout)
        verdict = payload.get('structured_output') or json.loads(payload.get('result', ''))
        assert verdict['verdict'] in ('pass', 'fail')
        return verdict
    except (ValueError, KeyError, TypeError, AssertionError):
        return {'verdict': 'fail', 'reason': f'critic returned no verdict (exit {result.returncode}): '
                f'{(result.stdout or result.stderr)[-500:]}', 'missing': []}


def run_one(question, skill, env, cwd, out, options):
    started = time.monotonic()
    try:
        result, events, final = ask(question, skill, env, cwd, options.model, options.timeout)
    except subprocess.TimeoutExpired:
        return {'id': question['id'], 'verdict': 'fail', 'reason': 'agent timed out', 'missing': []}
    (out / f"{question['id']}.agent.jsonl").write_text(result.stdout)
    answer = final.get('result') or ''
    denials = final.get('permission_denials') or []
    calls, outside, loaded = audit(events, skill, cwd.resolve(), denials)
    record = {'id': question['id'], 'question': question['question'].strip(), 'answer': answer,
              'skill_loaded': loaded, 'tool_calls': len(calls), 'outside_skill': outside,
              'permission_denials': denials, 'agent_exit': result.returncode}
    if result.returncode != 0 or not answer:
        record.update(verdict='fail', reason=f'agent failed (exit {result.returncode}): {result.stderr[-500:]}', missing=[])
    elif outside:
        record.update(verdict='fail', reason='agent read outside the skill', missing=[])
    else:
        record.update(grade(question, answer, env, cwd, options.critic_model, options.timeout))
    record['seconds'] = round(time.monotonic() - started)
    (out / f"{question['id']}.result.json").write_text(json.dumps(record, indent=2) + '\n')
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    parser.add_argument('skill', type=Path, help='skill directory under test (contains SKILL.md)')
    parser.add_argument('--questions', type=Path, default=HERE / 'archon-questions.toml')
    parser.add_argument('--only', nargs='*', default=[], help='question ids to run')
    parser.add_argument('--out', type=Path, help='transcript directory (default: a new temporary directory)')
    parser.add_argument('--model', help='model for the agent under test (default: harness default)')
    parser.add_argument('--critic-model', help='model for the critic (default: harness default)')
    parser.add_argument('--jobs', type=int, default=4)
    parser.add_argument('--timeout', type=int, default=600, help='seconds per claude call')
    options = parser.parse_args()
    skill = options.skill.resolve()
    if not (skill / 'SKILL.md').is_file():
        parser.error(f'{skill} has no SKILL.md')
    questions = tomllib.loads(options.questions.read_text())['question']
    if options.only:
        unknown = set(options.only) - {q['id'] for q in questions}
        if unknown:
            parser.error(f'unknown question ids: {", ".join(sorted(unknown))}')
        questions = [q for q in questions if q['id'] in options.only]
    out = (options.out or Path(tempfile.mkdtemp(prefix='archon-skill-eval-'))).resolve()
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='archon-skill-eval-run-') as temporary:
        root = Path(temporary)
        home, cwd = isolated_home(root, skill), root / 'cwd'
        cwd.mkdir()
        env = clean_env(home)
        print(f'skill: {skill}\nagent: claude -p, HOME={home} (skills: archon only), cwd={cwd}\n'
              f'access: --tools {AGENT_TOOLS} --permission-mode dontAsk, reads allowed under the skill only\n'
              f'transcripts: {out}\n', flush=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=options.jobs) as pool:
            sandbox = pool.submit(canary, root, skill, env, cwd, options.model, options.timeout, out)
            futures = {pool.submit(run_one, q, skill, env, cwd, out, options): q['id'] for q in questions}
            results = []
            for future in concurrent.futures.as_completed(futures):
                record = future.result()
                results.append(record)
                note = '' if record['verdict'] == 'pass' else f": {record['reason']}"
                print(f"{record['verdict'].upper():4}  {record['id']}{note}", flush=True)
            sandbox = sandbox.result()
    order = {q['id']: i for i, q in enumerate(questions)}
    results.sort(key=lambda r: order[r['id']])
    passed = sum(r['verdict'] == 'pass' for r in results)
    summary = {'skill': str(skill), 'passed': passed, 'total': len(results), 'sandbox': sandbox, 'results': results}
    (out / 'results.json').write_text(json.dumps(summary, indent=2) + '\n')
    lines = [f"{r['verdict'].upper():4}  {r['id']}  (skill loaded: {'yes' if r.get('skill_loaded') else 'no'}, "
             f"tool calls: {r.get('tool_calls', 0)}, refused: {len(r.get('permission_denials', []))}, "
             f"read outside skill: {len(r.get('outside_skill', []))})" for r in results]
    lines.append(f"SANDBOX {'held' if sandbox['held'] else 'BROKEN'}: a read outside the skill was "
                 f"{'refused' if sandbox['refused'] else 'not refused'} and its contents "
                 f"{'leaked' if sandbox['leaked'] else 'did not leak'}")
    lines.append(f'TOTAL {passed}/{len(results)}')
    (out / 'summary.txt').write_text('\n'.join(lines) + '\n')
    print('\n' + '\n'.join(lines))
    return 0 if passed == len(results) and sandbox['held'] else 1


if __name__ == '__main__':
    sys.exit(main())
