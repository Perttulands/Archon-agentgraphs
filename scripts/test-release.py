#!/usr/bin/env python3
"""Verify a real release's installer, commands, UI and persisted runs."""
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout


def manifest(root):
    files = sorted(p for p in root.rglob('*') if p.is_file() and p.name != 'MANIFEST.sha256')
    (root / 'MANIFEST.sha256').write_text(''.join(
        f'{hashlib.sha256(p.read_bytes()).hexdigest()}  ./{p.relative_to(root)}\n' for p in files))


def get(url):
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(url, timeout=5) as response:
        return response.read()


def daemon(command, state):
    process = subprocess.Popen([str(command), '--executor', 'lab', '--state-dir', str(state),
                                '--listen', '127.0.0.1:0'], stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True, cwd='/')
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    if not selector.select(timeout=20):
        process.kill()
        process.wait()
        raise AssertionError('daemon did not become ready')
    line = process.stdout.readline()
    selector.close()
    match = re.search(r'http://127\.0\.0\.1:\d+', line)
    if not match:
        process.terminate()
        _, errors = process.communicate(timeout=15)
        raise AssertionError(f'daemon failed: {line} {errors}')
    return process, match[0]


def stop(process):
    process.terminate()
    _, errors = process.communicate(timeout=15)
    assert process.returncode == 0, errors


def check_skill(skill):
    """The installed release carries the archon agent skill with working links."""
    text = (skill / 'SKILL.md').read_text()
    front = re.match(r'---\n(.*?)\n---\n', text, re.S)
    assert front and re.search(r'^name: archon$', front[1], re.M), 'skill frontmatter must name archon'
    assert re.search(r'^description: \S', front[1], re.M), 'skill needs a description'
    for page in skill.rglob('*.md'):
        for target in re.findall(r'\]\(([^)#:]+\.md)\)', page.read_text()):
            assert (page.parent / target).is_file(), f'{page.name} links to missing {target}'


def main():
    archive = Path(sys.argv[1]).resolve()
    with tempfile.TemporaryDirectory(prefix='archon-release-test-') as temporary:
        root = Path(temporary)
        with tarfile.open(archive) as tar:
            tar.extractall(root, filter='data')
        bundle = next(p for p in root.iterdir() if p.is_dir())
        prefix = root / 'prefix with spaces'
        state = root / 'existing state'
        state.mkdir()
        sentinel = state / 'operator-data'
        sentinel.write_text('keep this data\n')
        # Model an earlier managed installation, then upgrade to this archive.
        previous = root / 'previous'
        shutil.copytree(bundle, previous)
        (previous / 'VERSION').write_text('0.0.0\n')
        manifest(previous)
        run(str(previous / 'install.sh'), '--prefix', str(prefix))
        old_target = (prefix / 'lib/archon/current').resolve()
        run(str(bundle / 'install.sh'), '--prefix', str(prefix))
        current = (prefix / 'lib/archon/current').resolve()
        assert current != old_target and old_target.is_dir()
        run(str(bundle / 'install.sh'), '--prefix', str(prefix))
        assert current == (prefix / 'lib/archon/current').resolve()
        assert sentinel.read_text() == 'keep this data\n'
        version, commit = (bundle / 'VERSION').read_text().strip(), (bundle / 'COMMIT').read_text().strip()
        check_skill(prefix / 'lib/archon/current/share/archon/skills/archon')
        for command in ('archon', 'archond'):
            result = run(str(prefix / 'bin' / command), '--version', cwd='/')
            assert version in result and commit in result, result
        # A mission runs on the installed daemon and its history survives a restart.
        mission_dir = state / '.archon/missions'
        mission_dir.mkdir(parents=True)
        mission_dir.joinpath('hello.mission.toml').write_text('''schema = 1
id = "brd_hello"
slug = "hello"
title = "Hello"
rev = 1
[[inputCard]]
id = "inp_hello"
title = "Hello"
goal = "Return the supplied input"
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"
[formation.brief]
goal = "Return a short result using the supplied output contract."
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Result"
[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
effort = "medium"
controller = true
[[connection]]
id = "edge_start"
from = "inp_hello:out"
to = "fmn_work:port_in"
''')
        cli = str(prefix / 'bin/archon')
        run(cli, '--workspace', str(state), 'mission', 'validate', 'hello', '--json')
        process, url = daemon(prefix / 'bin/archond', state)
        try:
            assert json.loads(get(url + '/healthz'))['data']['status'] == 'ok'
            html = get(url + '/').decode()
            assert '<title>Archon</title>' in html
            for asset in re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html):
                assert len(get(url + asset)) > 0
            receipt = json.loads(run(cli, '--server', url, 'mission', 'run', 'hello',
                                     '--cwd', str(root),
                                     '--input', 'brief=Archon release smoke', '--json'))
            run_id = receipt['data']['runId']
            follow = run(cli, '--server', url, 'run', 'follow', run_id, '--json', timeout=20)
            assert 'succeeded' in follow, follow
        finally:
            stop(process)
        process, url = daemon(prefix / 'bin/archond', state)
        try:
            status = json.loads(run(cli, '--server', url, 'run', 'status', run_id, '--json'))
            assert status['data']['status'] == 'succeeded', status
            assert run_id.encode() in get(url + '/api/runs')
        finally:
            stop(process)
        # Reject a corrupted bundle before it replaces a managed installation.
        (bundle / 'bin/archon').write_bytes(b'corrupt')
        failed = subprocess.run([str(bundle / 'install.sh'), '--prefix', str(prefix)], capture_output=True)
        assert failed.returncode != 0 and (prefix / 'lib/archon/current').resolve() == current
        # Reject unmanaged binaries without altering their contents.
        other = root / 'unmanaged'
        (other / 'bin').mkdir(parents=True)
        (other / 'bin/archon').write_text('operator binary')
        failed = subprocess.run([str(previous / 'install.sh'), '--prefix', str(other)], capture_output=True)
        assert failed.returncode != 0 and (other / 'bin/archon').read_text() == 'operator binary'
        foreign = root / 'foreign-current'
        operator_dir = foreign / 'lib/archon/releases/operator-files'
        operator_dir.mkdir(parents=True)
        (operator_dir / 'keep').write_text('operator files')
        current_link = foreign / 'lib/archon/current'
        current_link.symlink_to('releases/operator-files')
        failed = subprocess.run([str(previous / 'install.sh'), '--prefix', str(foreign)], capture_output=True)
        assert failed.returncode != 0 and os.readlink(current_link) == 'releases/operator-files'
        assert (operator_dir / 'keep').read_text() == 'operator files'
        assert sentinel.read_text() == 'keep this data\n'
        print(f'PASS {archive.name}: install, upgrade, reinstall, checksum rejection, unmanaged-file protection, UI, commands, persisted run, agent skill')


if __name__ == '__main__':
    main()
