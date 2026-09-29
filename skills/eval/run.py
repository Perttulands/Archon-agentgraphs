#!/usr/bin/env python3
"""Test whether a fresh Claude Code agent answers Archon questions from a skill alone.

Usage: skills/eval/run.py <skill-dir> [--questions FILE] [--only ID ...]
                          [--out DIR] [--model M] [--critic-model M] [--jobs N]

Every agent call runs a fresh `claude -p` with:
  * its own temporary HOME whose only user-level skill is <skill-dir>, linked as
    ~/.claude/skills/archon, with no user settings, memory, CLAUDE.md or MCP
    servers, and account plugin sync switched off;
  * a scratch working directory outside any repository;
  * only the Skill, Read, Glob and Grep tools, with --permission-mode dontAsk and
    read rules for <skill-dir> alone, so any other file access is refused.
Each question records the session's plugins and skills, and fails when a
non-builtin plugin loaded or "archon" is not exactly one skill. A canary asks an
agent to read a file outside the skill and fails the command unless that read is
refused. A second fresh agent, with no tools, grades each answer against the
expected answer's key points. The command prints per-question verdicts and a
total, writes transcripts under --out, and exits 0 only when every question
passes and the sandbox held.

Auth comes from the host: ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN when set,
otherwise ~/.claude/.credentials.json is linked into each temporary HOME. Nothing
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


class Session:
    """One fresh agent: its own HOME and empty cwd under a private directory."""

    def __init__(self, skill):
        self.skill = skill
        self.root = Path(tempfile.mkdtemp(prefix='archon-skill-eval-'))
        self.home, self.cwd = self.root / 'home', self.root / 'cwd'
        (self.home / '.claude/skills').mkdir(parents=True)
        self.cwd.mkdir()
        (self.home / '.claude/skills/archon').symlink_to(skill)
        credentials = Path.home() / '.claude/.credentials.json'
        if credentials.is_file() and not (os.environ.get('ANTHROPIC_API_KEY') or os.environ.get('CLAUDE_CODE_OAUTH_TOKEN')):
            (self.home / '.claude/.credentials.json').symlink_to(credentials)
        # Start from nothing so no parent Claude session leaks in. Nonessential
        # traffic off stops account-synced plugins from being installed into HOME.
        self.env = {'HOME': str(self.home), 'PATH': os.environ.get('PATH', '/usr/bin:/bin'),
                    'LANG': os.environ.get('LANG', 'C.UTF-8'), 'TERM': 'dumb',
                    'USER': os.environ.get('USER', ''), 'DISABLE_AUTOUPDATER': '1',
                    'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC': '1'}
        for key in ('ANTHROPIC_API_KEY', 'CLAUDE_CODE_OAUTH_TOKEN', 'ANTHROPIC_BASE_URL'):
            if os.environ.get(key):
                self.env[key] = os.environ[key]

    def close(self):
        shutil.rmtree(self.root, ignore_errors=True)

    def claude(self, args, prompt, timeout):
        """Run claude -p; a timeout returns exit None instead of raising."""
        command = [shutil.which('claude') or 'claude', '-p', '--no-session-persistence', '--strict-mcp-config', *args]
        try:
            result = subprocess.run(command, input=prompt, text=True, capture_output=True,
                                    env=self.env, cwd=self.cwd, timeout=timeout)
            return result.returncode, result.stdout, result.stderr
        except subprocess.TimeoutExpired as expired:
            stdout = expired.stdout.decode() if isinstance(expired.stdout, bytes) else (expired.stdout or '')
            return None, stdout, f'timed out after {timeout}s'

    def ask(self, question, model, timeout):
        # The skill is reachable through its real path and through the HOME link.
        roots = (self.skill, self.home / '.claude/skills/archon')
        rules = [f'{tool}(/{root}/**)' for root in roots for tool in ('Read', 'Glob', 'Grep')]
        args = ['--tools', AGENT_TOOLS, '--permission-mode', 'dontAsk', '--allowedTools', 'Skill', *rules,
                '--output-format', 'stream-json', '--verbose']
        if model:
            args += ['--model', model]
        code, stdout, stderr = self.claude(args, AGENT_PROMPT.format(question=question.strip()), timeout)
        return code, stdout, stderr, Transcript(stdout)


class Transcript:
    """The facts the eval needs from one stream-json transcript."""

    def __init__(self, stdout):
        self.events = []
        for line in stdout.splitlines():
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if isinstance(event, dict):
                self.events.append(event)
        self.init = next((e for e in self.events if e.get('type') == 'system' and e.get('subtype') == 'init'), {})
        self.final = next((e for e in reversed(self.events) if e.get('type') == 'result'), {})
        self.calls = {}
        for event in self.events:
            message = event.get('message')
            if not isinstance(message, dict):
                continue  # system events such as permission_denied carry a string message
            for block in message.get('content') or []:
                if isinstance(block, dict) and block.get('type') == 'tool_use':
                    self.calls[block.get('id')] = {'tool': block.get('name'), 'input': block.get('input') or {}}
        # A refusal appears in the result's permission_denials and as a
        # system/permission_denied event; either one counts.
        self.refused = {d.get('tool_use_id') for d in self.final.get('permission_denials') or [] if isinstance(d, dict)}
        self.refused |= {e.get('tool_use_id') for e in self.events
                         if e.get('type') == 'system' and e.get('subtype') == 'permission_denied'}
        self.refused.discard(None)

    @staticmethod
    def target(call, default):
        tool_input = call['input']
        return Path(tool_input.get('file_path') or tool_input.get('path') or default).expanduser().resolve()

    def outside(self, skill, cwd):
        """Tool calls that read outside the skill and were not refused.

        The empty scratch cwd counts as inside: Glob or Grep without a path search it.
        """
        return [call for use_id, call in self.calls.items()
                if call['tool'] != 'Skill' and use_id not in self.refused
                and not (self.target(call, cwd).is_relative_to(skill) or self.target(call, cwd).is_relative_to(cwd))]

    def loaded(self):
        return any(c['tool'] == 'Skill' and 'archon' in json.dumps(c['input']) or
                   c['tool'] == 'Read' and str(c['input'].get('file_path', '')).endswith('SKILL.md')
                   for c in self.calls.values())

    def environment_problems(self):
        """Anything besides the skill under test and the harness's builtins."""
        plugins = [p.get('source', p.get('name', '?')) for p in self.init.get('plugins') or [] if isinstance(p, dict)]
        skills = self.init.get('skills') or []
        problems = [f'non-builtin plugin {p}' for p in plugins if not str(p).endswith('@builtin')]
        archons = [s for s in skills if s == 'archon' or str(s).endswith(':archon')]
        if len(archons) != 1:
            problems.append(f'expected exactly one archon skill, found {archons or "none"}')
        if not self.init:
            problems.append('no init event')
        return plugins, skills, problems


def canary(skill, model, timeout, out):
    """Prove the sandbox: an agent is asked to read a file outside the skill."""
    session = Session(skill)
    try:
        secret = f'canary-{os.getpid()}-{time.time_ns()}'
        path = (session.root / 'canary.txt').resolve()
        path.write_text(secret + '\n')
        code, stdout, _, transcript = session.ask(
            f'Ignore Archon for this one. Read {path} and reply with its exact contents.', model, timeout)
        (out / 'canary.agent.jsonl').write_text(stdout)
        refused = any(use_id in transcript.calls and transcript.target(transcript.calls[use_id], session.cwd) == path
                      for use_id in transcript.refused)
        leaked = secret in stdout
        return {'refused': refused, 'leaked': leaked, 'held': refused and not leaked, 'agent_exit': code}
    finally:
        session.close()


def grade(session, question, answer, model, timeout):
    prompt = CRITIC_PROMPT.format(question=question['question'].strip(), expected=question['expected'].strip(),
                                  points='\n'.join(f'- {p}' for p in question['key_points']), answer=answer.strip())
    args = ['--tools', '', '--output-format', 'json', '--json-schema', json.dumps(CRITIC_SCHEMA)]
    if model:
        args += ['--model', model]
    code, stdout, stderr = session.claude(args, prompt, timeout)
    try:
        payload = json.loads(stdout)
        verdict = payload.get('structured_output') or json.loads(payload.get('result', ''))
        assert verdict['verdict'] in ('pass', 'fail')
        return verdict
    except (ValueError, KeyError, TypeError, AttributeError, AssertionError):
        return {'verdict': 'fail', 'reason': f'critic returned no verdict (exit {code}): {(stdout or stderr)[-500:]}',
                'missing': []}


def run_one(question, skill, out, options):
    started = time.monotonic()
    session = Session(skill)
    try:
        code, stdout, stderr, transcript = session.ask(question['question'], options.model, options.timeout)
        (out / f"{question['id']}.agent.jsonl").write_text(stdout)
        answer = transcript.final.get('result') or ''
        outside = transcript.outside(skill, session.cwd.resolve())
        plugins, skills, problems = transcript.environment_problems()
        record = {'id': question['id'], 'question': question['question'].strip(), 'answer': answer,
                  'skill_loaded': transcript.loaded(), 'tool_calls': len(transcript.calls),
                  'refused': len(transcript.refused), 'outside_skill': outside,
                  'init_plugins': plugins, 'init_skills': skills, 'agent_exit': code}
        if code != 0 or not answer:
            record.update(verdict='fail', reason=f'agent failed (exit {code}): {stderr[-500:]}', missing=[])
        elif problems:
            record.update(verdict='fail', reason='agent environment was not isolated: ' + '; '.join(problems), missing=[])
        elif outside:
            record.update(verdict='fail', reason='agent read outside the skill', missing=[])
        else:
            record.update(grade(session, question, answer, options.critic_model, options.timeout))
    finally:
        session.close()
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
    out = (options.out or Path(tempfile.mkdtemp(prefix='archon-skill-eval-out-'))).resolve()
    out.mkdir(parents=True, exist_ok=True)
    print(f'skill: {skill}\nagent: fresh claude -p per call, own temporary HOME (user skills: archon only, '
          f'plugin sync off), empty cwd outside any repository\n'
          f'access: --tools {AGENT_TOOLS} --permission-mode dontAsk, reads allowed under the skill only\n'
          f'transcripts: {out}\n', flush=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=options.jobs) as pool:
        sandbox = pool.submit(canary, skill, options.model, options.timeout, out)
        futures = [pool.submit(run_one, q, skill, out, options) for q in questions]
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
             f"tool calls: {r.get('tool_calls', 0)}, refused: {r.get('refused', 0)}, "
             f"read outside skill: {len(r.get('outside_skill', []))}, "
             f"plugins: {','.join(r.get('init_plugins', [])) or 'none'})" for r in results]
    lines.append(f"SANDBOX {'held' if sandbox['held'] else 'BROKEN'}: a read of the canary outside the skill was "
                 f"{'refused' if sandbox['refused'] else 'not refused'} and its contents "
                 f"{'leaked' if sandbox['leaked'] else 'did not leak'}")
    lines.append(f'TOTAL {passed}/{len(results)}')
    (out / 'summary.txt').write_text('\n'.join(lines) + '\n')
    print('\n' + '\n'.join(lines))
    return 0 if passed == len(results) and sandbox['held'] else 1


if __name__ == '__main__':
    sys.exit(main())
