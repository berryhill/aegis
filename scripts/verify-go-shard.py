#!/usr/bin/env python3
"""Exact-once bounded Go package groups; invoke through verify-budget.py.

Six groups isolate measured slow packages without duplicating compilation or
splitting TestMain, examples, fuzz seeds, or subtests. The remainder is exact set
subtraction from `go list ./...`; new packages cannot silently escape coverage.
"""
import argparse
import json
import subprocess
import sys

ROOT = 'github.com/berryhill/aegis/'
GROUPS = {
    'api': ('internal/api',),
    'execution': ('internal/implementation', 'internal/orchestration'),
    'cli': ('cmd/aegis', 'internal/command', 'internal/console'),
    'persistence': ('internal/persistence/fleet/badger', 'internal/skillbundle', 'internal/skillbundle/cmd'),
    'remainder-a': (),
    'remainder-b': (),
}


def inventory(text):
    packages = text.splitlines()
    if not packages or len(packages) != len(set(packages)):
        raise ValueError('empty or duplicate Go package inventory')
    for package in packages:
        if not package.startswith(ROOT) or package != package.strip() or any(c.isspace() for c in package) or not package[len(ROOT):]:
            raise ValueError('unknown or malformed Go package: ' + repr(package))
    return sorted(packages)


def groups(packages):
    if not packages or len(packages) != len(set(packages)):
        raise ValueError('empty or duplicate package inventory')
    selected = {name: [ROOT + path for path in paths] for name, paths in GROUPS.items()}
    assigned = [package for names in selected.values() for package in names]
    if len(assigned) != len(set(assigned)) or not set(assigned) <= set(packages):
        raise ValueError('configured package missing or assigned more than once')
    remainder = sorted(set(packages) - set(assigned))
    selected['remainder-a'] = remainder[::2]
    selected['remainder-b'] = remainder[1::2]
    union = [package for names in selected.values() for package in names]
    if len(union) != len(packages) or set(union) != set(packages):
        raise ValueError('package coverage mismatch')
    return selected


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--group', choices=GROUPS, required=True)
    parser.add_argument('--race', action='store_true')
    parser.add_argument('--list-only', action='store_true')
    args = parser.parse_args(argv)
    result = subprocess.run(['go', 'list', './...'], text=True, stdout=subprocess.PIPE, check=True)
    packages = inventory(result.stdout)
    selected = groups(packages)[args.group]
    if not selected:
        raise ValueError('selected group has no packages')
    command = ['go', 'test', *(['-race'] if args.race else []), '-count=1', '-timeout=8m', *selected]
    print(json.dumps({'group': args.group, 'race': args.race, 'total_packages': len(packages), 'selected_packages': len(selected), 'command': command}), flush=True)
    if not args.list_only:
        subprocess.run(command, check=True)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print('Go shard denied: ' + str(error), file=sys.stderr)
        sys.exit(1)
