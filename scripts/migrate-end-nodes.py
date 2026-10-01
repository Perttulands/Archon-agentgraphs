#!/usr/bin/env python3
"""One-time migration to End nodes (form-o7p.10). Delete this script once the
live state has been migrated.

Every formation output and gate pass or fail route must now lead to a step, a
gate or an End node. This script finds each route that leads nowhere in every
mission of an Archon state directory and ends it on purpose: an output or a
pass gets its own Done End node, a fail its own Rejected End node, placed to
the right of the node it ends. Writes go through the archon CLI, so they are
validated, revisioned and laid out like any authoring.

Stop the daemon (or run before it starts), then:

    scripts/migrate-end-nodes.py --archon /path/to/archon --workspace <state> [--dry-run]

It reads mission files from <state>/.formations/boards/*.formation.toml (or,
after the rename, <state>/.archon/missions/*.mission.toml) and is idempotent:
a second run finds nothing to do.
"""

import argparse
import glob
import json
import os
import subprocess
import sys
import tomllib

END_WIDTH, END_HEIGHT = 148, 64
CARD_WIDTH, CARD_HEIGHT = 330, 340
GAP = 84


def mission_files(workspace):
    found = []
    for pattern, suffix in (
        (".formations/boards/*.formation.toml", ".formation.toml"),
        (".archon/missions/*.mission.toml", ".mission.toml"),
    ):
        for path in sorted(glob.glob(os.path.join(workspace, pattern))):
            found.append((os.path.basename(path)[: -len(suffix)], path))
    return found


def layout_positions(mission_path):
    directory = os.path.dirname(os.path.dirname(mission_path))
    slug = os.path.basename(mission_path).split(".")[0]
    for candidate in (
        os.path.join(directory, "layout", slug + ".layout.toml"),
        os.path.join(directory, "layouts", slug + ".layout.toml"),
    ):
        if os.path.exists(candidate):
            with open(candidate, "rb") as handle:
                layout = tomllib.load(handle)
            return {node["id"]: (node.get("x", 0), node.get("y", 0)) for node in layout.get("node", [])}
    return {}


def dangling_routes(mission):
    wired = {connection.get("from") for connection in mission.get("connection", [])}
    routes = []
    for formation in mission.get("formation", []):
        for port in formation.get("output", []):
            endpoint = formation["id"] + ":" + port["id"]
            if endpoint not in wired:
                routes.append((formation["id"], endpoint, "done"))
    for gate in mission.get("gate", []):
        for port, outcome in (("pass", "done"), ("fail", "rejected")):
            endpoint = gate["id"] + ":" + port
            if endpoint not in wired:
                routes.append((gate["id"], endpoint, outcome))
    return routes


def overlaps(x, y, occupied):
    for ox, oy in occupied:
        if x < ox + CARD_WIDTH and ox < x + END_WIDTH and y < oy + CARD_HEIGHT and oy < y + END_HEIGHT:
            return True
    return False


def place(source, outcome, positions, occupied):
    sx, sy = positions.get(source, (140, 168))
    x = sx + CARD_WIDTH + GAP
    y = sy + (150 if outcome == "rejected" else 0)
    while overlaps(x, y, occupied):
        y += END_HEIGHT + 28
    occupied.append((x, y))
    return x, y


def archon(binary, workspace, *args):
    result = subprocess.run([binary, "--workspace", workspace, *args], capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f"archon {' '.join(args)} exited {result.returncode}: {result.stderr.strip()}")
    return result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--archon", required=True, help="archon CLI binary built from the End node release")
    parser.add_argument("--workspace", required=True, help="Archon state directory (the daemon's --workspace)")
    parser.add_argument("--dry-run", action="store_true", help="report the routes without writing")
    options = parser.parse_args()

    files = mission_files(options.workspace)
    if not files:
        print(f"no missions under {options.workspace}", file=sys.stderr)
        return 1
    changed = 0
    for slug, path in files:
        with open(path, "rb") as handle:
            mission = tomllib.load(handle)
        routes = dangling_routes(mission)
        if not routes:
            print(f"{slug}: every route leads somewhere")
            continue
        positions = layout_positions(path)
        occupied = list(positions.values())
        for source, endpoint, outcome in routes:
            x, y = place(source, outcome, positions, occupied)
            title = "Done" if outcome == "done" else "Rejected"
            if options.dry_run:
                print(f"{slug}: would end {endpoint} at a new {title} End node ({x}, {y})")
                continue
            created = json.loads(archon(options.archon, options.workspace, "end", "create", slug,
                                        "--outcome", outcome, "--title", title, "--x", str(x), "--y", str(y),
                                        "--updated-by", "agent:migrate-end-nodes", "--json"))
            end_id = created["end"]["id"]
            archon(options.archon, options.workspace, "formation", "wire", slug, endpoint, end_id + ":in",
                   "--updated-by", "agent:migrate-end-nodes")
            print(f"{slug}: {endpoint} -> {end_id} ({title})")
            changed += 1
        if not options.dry_run:
            with open(path, "rb") as handle:
                left = dangling_routes(tomllib.load(handle))
            if left:
                raise RuntimeError(f"{slug}: routes still lead nowhere after migration: {left}")
    print(f"{changed} route(s) now end at End nodes" if not options.dry_run else "dry run: nothing written")
    return 0


if __name__ == "__main__":
    sys.exit(main())
