#!/usr/bin/env python3
"""Bounded Go groups with dynamically complete, exact-once default tests.

API alternates the actual mode's sorted default inventory; orchestration isolates
the measured contribution integration and assigns every other current/future
test, runnable example and fuzz target to its remainder. Manager isolates its
three measured contribution integrations with the same inventory mechanism.
All children/seeds run.
Benchmarks are inventoried but not run by default. TestMain runs in each process.
All other packages remain exact-once, including future packages.
"""
import argparse
import json
import re
from collections import Counter
import subprocess
import sys

ROOT = 'github.com/berryhill/aegis/'
API = ROOT + 'internal/api'
ORCHESTRATION = ROOT + 'internal/orchestration'
CONTRIBUTION_TEST = 'TestContributionQueueNativeRepairIntegration'
MANAGER = ROOT + 'internal/managergateway'
MANAGER_TESTS = {
    'manager-contribution-repair': 'TestContributionManagerApprovedNativeRepair',
    'manager-contribution-disconnect': 'TestContributionManagerDisconnectKeepsValidAuthorityExecution',
    'manager-contribution-resume': 'TestContributionManagerExpiredFrozenCandidateExplicitFreshResume',
}
GROUPS = {
    'api-a': ('internal/api',),
    'api-b': ('internal/api',),
    # Measured package deadline: isolate the dominant contribution integration.
    'implementation': ('internal/implementation',),
    'orchestration': ('internal/orchestration',),
    'orchestration-contribution': ('internal/orchestration',),
    'manager': ('internal/managergateway',),
    **{group: ('internal/managergateway',) for group in MANAGER_TESTS},
    'cli': ('cmd/aegis', 'internal/command', 'internal/console'),
    'persistence': ('internal/persistence/fleet/badger', 'internal/skillbundle', 'internal/skillbundle/cmd'),
    # CI race timing: update alone takes 368-373s; never pair it with the remainder.
    'update': ('internal/update',),
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
    expected = Counter({p: 1 for p in set(assigned)})
    expected[API] = 2
    expected[ORCHESTRATION] = 2
    expected[MANAGER] = 4
    if Counter(assigned) != expected or not set(assigned) <= set(packages):
        raise ValueError('configured package missing or assigned more than once')
    remainder = sorted(set(packages) - set(assigned))
    selected['remainder-a'] = remainder[::2]
    selected['remainder-b'] = remainder[1::2]
    union = [package for names in selected.values() for package in names]
    if Counter(union) != Counter(packages) + Counter({API: 1, ORCHESTRATION: 1, MANAGER: 3}):
        raise ValueError('package coverage mismatch')
    return selected


def test_inventory(text, package=API):
    """Fail closed on missing footer, empty/duplicate names or unknown output."""
    lines = text.splitlines()
    if not lines or not re.fullmatch(r'ok\s+' + re.escape(package) + r'\s+\d+(?:\.\d+)?s', lines[-1]):
        raise ValueError('missing Go test inventory completion')
    names = lines[:-1]
    if not names or len(names) != len(set(names)):
        raise ValueError('empty or duplicate Go test inventory')
    if any(not re.fullmatch(r'(?:Test|Example|Fuzz|Benchmark)\w*', name) for name in names):
        raise ValueError('unknown Go test inventory entry')
    defaults = sorted(name for name in names if not name.startswith('Benchmark'))
    if len(defaults) < 2:
        raise ValueError('empty Go test partition')
    return defaults


def test_pattern(names, group):
    if not names or len(names) != len(set(names)):
        raise ValueError('empty or duplicate default test inventory')
    if group in ('api-a', 'api-b'):
        selected = names[0 if group == 'api-a' else 1::2]
    elif group in ('orchestration', 'orchestration-contribution'):
        if CONTRIBUTION_TEST not in names:
            raise ValueError('missing contribution integration test')
        selected = [name for name in names if (name == CONTRIBUTION_TEST) == (group == 'orchestration-contribution')]
    elif group == 'manager' or group in MANAGER_TESTS:
        if not set(MANAGER_TESTS.values()) <= set(names):
            raise ValueError('missing manager contribution integration test')
        selected = ([MANAGER_TESTS[group]] if group in MANAGER_TESTS else
                    [name for name in names if name not in MANAGER_TESTS.values()])
    else:
        raise ValueError('unknown test partition')
    if not selected:
        raise ValueError('empty Go test partition')
    # No slash: matching a parent deliberately executes all subtests/fuzz seeds.
    return '^(?:' + '|'.join(re.escape(name) for name in selected) + ')$'


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
    mode = ['-race'] if args.race else []
    api = args.group in ('api-a', 'api-b')
    timeout = '12m' if args.race and api else '8m'
    command = ['go', 'test', *mode, '-count=1', '-timeout=' + timeout]
    if api or args.group in ('orchestration', 'orchestration-contribution', 'manager') or args.group in MANAGER_TESTS:
        package = API if api else MANAGER if args.group == 'manager' or args.group in MANAGER_TESTS else ORCHESTRATION
        listing = subprocess.run([*command, '-list', '.', package], text=True, stdout=subprocess.PIPE, check=True)
        names = test_inventory(listing.stdout, package)
        command += ['-run', test_pattern(names, args.group)]
    command += selected
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
