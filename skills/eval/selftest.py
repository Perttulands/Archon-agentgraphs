#!/usr/bin/env python3
"""Self-test for run.py: a refused read outside the skill is counted, not fatal.

Runs run.py on selftest-questions.toml, whose one question makes the agent try a
Read outside the skill. Passes when run.py exits 0 and results.json shows the
refusal (refused > 0), no unrefused outside read, an isolated environment and a
held sandbox. Uses the host's Claude auth, like run.py.
"""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent


def main():
    skill = Path(sys.argv[1]) if len(sys.argv) > 1 else HERE.parent / 'archon'
    with tempfile.TemporaryDirectory(prefix='archon-skill-selftest-') as out:
        run = subprocess.run([sys.executable, str(HERE / 'run.py'), str(skill), '--questions',
                              str(HERE / 'selftest-questions.toml'), '--out', out])
        results = json.loads((Path(out) / 'results.json').read_text())
        record = results['results'][0]
        checks = {
            'run.py exited 0': run.returncode == 0,
            'the outside read was refused (refused > 0)': record.get('refused', 0) > 0,
            'no unrefused read outside the skill': record.get('outside_skill') == [],
            'only builtin plugins loaded': all(p.endswith('@builtin') for p in record.get('init_plugins', [])),
            'the canary sandbox held': results['sandbox']['held'],
            'the question passed': record.get('verdict') == 'pass',
        }
    for name, ok in checks.items():
        print(f"{'ok  ' if ok else 'FAIL'}  {name}")
    return 0 if all(checks.values()) else 1


if __name__ == '__main__':
    sys.exit(main())
