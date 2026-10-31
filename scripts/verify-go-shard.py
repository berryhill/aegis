#!/usr/bin/env python3
"""Exact-once dynamic Go package/default-test groups, planned before execution.

API alternates default inventory. Implementation/orchestration isolate measured
contribution tests. Manager isolates the bounded native contribution journeys. All children,
examples and default fuzz seeds remain covered; TestMain runs in each process.
"""
import argparse
import json
import re
from collections import Counter
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = 'github.com/berryhill/aegis/'
API = ROOT + 'internal/api'
IMPLEMENTATION = ROOT + 'internal/implementation'
IMPLEMENTATION_TEST = 'TestManagedContributionRepairAndCustody'
ORCHESTRATION = ROOT + 'internal/orchestration'
IMPLEMENTATION = ROOT + 'internal/implementation'
CONTRIBUTION_TEST = 'TestContributionQueueNativeRepairIntegration'
IMPLEMENTATION_CONTRIBUTION_TEST = 'TestManagedContributionRepairAndCustody'
PARTITIONS = {
    'orchestration': (ORCHESTRATION, CONTRIBUTION_TEST),
    'orchestration-contribution': (ORCHESTRATION, CONTRIBUTION_TEST),
    'implementation': (IMPLEMENTATION, IMPLEMENTATION_CONTRIBUTION_TEST),
    'implementation-contribution': (IMPLEMENTATION, IMPLEMENTATION_CONTRIBUTION_TEST),
}
MANAGER = ROOT + 'internal/managergateway'
MANAGER_TESTS = {
    'manager-contribution-repair': 'TestContributionManagerApprovedNativeRepair',
    'manager-contribution-disconnect': 'TestContributionManagerDisconnectKeepsValidAuthorityExecution',
    'manager-contribution-resume': 'TestContributionManagerExpiredFrozenCandidateExplicitFreshResume',
    'manager-contribution-restart': 'TestContributionManagerHardRestartFrozenCandidateResume',
    'manager-contribution-joint': 'TestContributionDeliveryJointNative',
    'manager-contribution-cancel': 'TestContributionDeliveryJointNativeNonlocalCancellation',
    'manager-contribution-final': 'TestManagerJoinedContributionCompletion',
    'manager-contribution-readback': 'TestManagerTerminalContributionReadback',
}
COMMAND = ROOT + 'internal/command'
CLI_CONTRIBUTION_TESTS = {'cli-contribution-root': 'TestPublicRootAuthenticatedContributionJourney', 'cli-contribution-final': 'TestPublicRootAuthenticatedContributionFinalJourney', 'cli-contribution-disconnect': 'TestPublicRootAuthenticatedContributionFinalDisconnectResumeJourney', 'cli-contribution-approval': 'TestPublicRootContributionDeclaredOperationApprovalHeld'}
GROUPS = {
    'api-a': ('internal/api',),
    'api-b': ('internal/api',),
    # Contribution repair/custody measured 54.43s normal; preserve the 8m cap.
    'implementation': ('internal/implementation',),
    'implementation-contribution': ('internal/implementation',),
    'orchestration': ('internal/orchestration',),
    'orchestration-contribution': ('internal/orchestration',),
    'manager': ('internal/managergateway',),
    **{group: ('internal/managergateway',) for group in MANAGER_TESTS},
    'cli': ('cmd/aegis', 'internal/console'),
    'cli-command': ('internal/command',),
    **{group: ('internal/command',) for group in CLI_CONTRIBUTION_TESTS},
    'persistence': ('internal/persistence/fleet/badger', 'internal/skillbundle', 'internal/skillbundle/cmd'),
    'update': ('internal/update',),
    'remainder-a': (),
    'remainder-b': (),
}


# Stable execution lanes, NOT new test partitions: 7 normal + 13 race.
# Hosted run 40888280401 hit unchanged 600s job deadlines in normal
# implementation, race orchestration and race manager-b. Repack measured
# phases into existing lanes/caps; fresh hosted deadline evidence is required.
# Future defaults still go through the dynamic remainder partitions above.
BUNDLES = {
    'normal': {
        'api-a': ('api-a', 'remainder-b'),
        'api-b': ('api-b', 'cli', 'persistence'),
        'implementation': ('implementation', 'implementation-contribution'),
        'orchestration': ('orchestration', 'orchestration-contribution', 'remainder-a'),
        'manager-a': ('manager', 'manager-contribution-restart', 'manager-contribution-final', 'manager-contribution-readback'),
        'manager-b': ('manager-contribution-repair', 'manager-contribution-disconnect', 'manager-contribution-resume', 'manager-contribution-joint', 'manager-contribution-cancel'),
        'cli': ('cli-command', *CLI_CONTRIBUTION_TESTS, 'update'),
    },
    'race': {
        # Hosted 40952580487 twice exhausted support-a: persistence 268s +
        # remainder-a 323s. Reuse api-a's existing 16m job (observed 479s),
        # without changing its 12m API or persistence's 8m process alarms.
        # Exact-head 41216964277: CLI compilation + its three passing roots
        # consumed 462s before remainder-b hit the unchanged 600s job limit.
        # This existing 16m lane completed in 521s on that same head. Move the
        # complete existing remainder group, not tests, caps or lane names.
        'api-a': ('api-a', 'persistence', 'remainder-b'),
        'api-b': ('api-b', 'cli'),
        # Hosted 41218036692: compile 65s + defaults 444s left too little
        # of 600s for contribution. Keep that group in a manager lane.
        'implementation': ('implementation',),
        'orchestration': ('orchestration', 'orchestration-contribution'),
        # Hosted 41220874653: manager-c used its full budget; manager-d
        # completed in 402s and already compiles implementation dependencies.
        'manager-a': ('manager',),
        'manager-b': ('manager-contribution-restart',),
        'manager-c': ('manager-contribution-resume', 'manager-contribution-readback'),
        'manager-d': ('manager-contribution-repair', 'manager-contribution-joint', 'manager-contribution-cancel', 'implementation-contribution'),
        'manager-e': ('manager-contribution-disconnect', 'cli-contribution-approval', 'manager-contribution-final'),
        # Hosted 41220874653: cli-command defaults took 462s plus 106s
        # compilation; support-a completed in 314s and takes disconnect.
        'cli-command': ('cli-command',),
        'cli': ('cli-contribution-root', 'cli-contribution-final'),
        'support-a': ('remainder-a', 'cli-contribution-disconnect'),
        'support-b': ('update',),
    },
}


def bundle_groups(name, race):
    mode = 'race' if race else 'normal'
    lanes = BUNDLES[mode]
    if Counter(g for lane in lanes.values() for g in lane) != Counter(GROUPS.keys()):
        raise ValueError('compact lane coverage mismatch')
    if name not in lanes:
        raise ValueError('unknown compact lane for mode')
    return lanes[name]


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
    expected.update({API: 1, ORCHESTRATION: 1, IMPLEMENTATION: 1, MANAGER: len(MANAGER_TESTS), COMMAND: len(CLI_CONTRIBUTION_TESTS)})
    if Counter(assigned) != expected or not set(assigned) <= set(packages):
        raise ValueError('configured package missing or assigned more than once')
    remainder = sorted(set(packages) - set(assigned))
    selected['remainder-a'] = remainder[::2]
    selected['remainder-b'] = remainder[1::2]
    union = [package for names in selected.values() for package in names]
    if Counter(union) != Counter(packages) + Counter({API: 1, ORCHESTRATION: 1, IMPLEMENTATION: 1, MANAGER: len(MANAGER_TESTS), COMMAND: len(CLI_CONTRIBUTION_TESTS)}):
        raise ValueError('package coverage mismatch')
    return selected


def test_inventory(text, package=API):
    lines = text.splitlines()
    if not lines or not re.fullmatch(r'ok\s+' + re.escape(package) + r'\s+\d+(?:\.\d+)?s', lines[-1]):
        raise ValueError('missing Go test inventory completion')
    return default_inventory(lines[:-1])


def default_inventory(names):
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
    elif group in PARTITIONS:
        _, target = PARTITIONS[group]
        if target not in names:
            raise ValueError('missing contribution integration test')
        selected = [name for name in names if (name == target) == group.endswith('-contribution')]
    elif group == 'cli-command' or group in CLI_CONTRIBUTION_TESTS:
        if not set(CLI_CONTRIBUTION_TESTS.values()) <= set(names):
            raise ValueError('missing CLI contribution integration test')
        selected = ([CLI_CONTRIBUTION_TESTS[group]] if group in CLI_CONTRIBUTION_TESTS else
                    [name for name in names if name not in CLI_CONTRIBUTION_TESTS.values()])
    elif group == 'manager' or group in MANAGER_TESTS:
        if not set(MANAGER_TESTS.values()) <= set(names):
            raise ValueError('missing manager contribution integration test')
        selected = ([MANAGER_TESTS[group]] if group in MANAGER_TESTS else
                    [name for name in names if name not in MANAGER_TESTS.values()])
    else:
        raise ValueError('unknown test partition')
    if not selected:
        raise ValueError('empty Go test partition')
    return '^(?:' + '|'.join(re.escape(name) for name in selected) + ')$'


def partition_package(group):
    if group in ('api-a', 'api-b'):
        return API
    if group in PARTITIONS:
        return PARTITIONS[group][0]
    if group == 'manager' or group in MANAGER_TESTS:
        return MANAGER
    if group == 'cli-command' or group in CLI_CONTRIBUTION_TESTS:
        return COMMAND
    return None


def timed_run(phase, command, **kwargs):
    start = time.monotonic()
    try:
        return subprocess.run(command, **kwargs)
    finally:
        # Separate compiler/inventory cost from uncached test execution in hosted
        # logs. These observations are not success evidence or timeout changes.
        sys.stderr.write(json.dumps({'go_lane_phase': phase,
                                     'seconds': round(time.monotonic() - start, 3)}) + '\n')


def run_bundle(name, race, list_only):
    selected_groups = bundle_groups(name, race)
    listing = subprocess.run(['go', 'list', './...'], text=True, stdout=subprocess.PIPE, check=True)
    packages = inventory(listing.stdout)
    grouped = groups(packages)
    mode = ['-race'] if race else []
    # Native Go test binaries preserve TestMain/default seeds and individual
    # process alarms. Link/list once per package/mode in this job, never launch
    # parallel children or combine partition alarms into a larger timeout.
    with tempfile.TemporaryDirectory(prefix='aegis-go-lane-') as directory:
        binaries, inventories, commands = {}, {}, []
        for group in selected_groups:
            selected = grouped[group]
            if not selected:
                raise ValueError('selected group has no packages')
            timeout = '12m' if race and group in ('api-a', 'api-b') else '8m'
            package = partition_package(group)
            cwd = None
            if package:
                if package not in binaries:
                    binary = str(Path(directory) / (str(len(binaries)) + '.test'))
                    # Ordinary `go test` links with -s -w, including race mode.
                    # Preserve that artifact shape when compiling once: these
                    # fixtures repeatedly inspect/hash their executing image.
                    timed_run('compile:' + package, ['go', 'test', *mode, '-c', '-ldflags=-s -w', '-o', binary, package], check=True)
                    result = timed_run('inventory:' + package, [binary, '-test.paniconexit0', '-test.list=.', '-test.timeout=' + timeout],
                                            cwd=Path(package[len(ROOT):]), text=True, stdout=subprocess.PIPE, check=True)
                    inventories[package] = default_inventory(result.stdout.splitlines())
                    binaries[package] = binary
                # Match go test's unexpected os.Exit(0) guard without rejecting
                # the normal TestMain os.Exit(m.Run()) after testing completes.
                command = [binaries[package], '-test.paniconexit0', '-test.count=1', '-test.timeout=' + timeout,
                           '-test.run=' + test_pattern(inventories[package], group)]
                cwd = Path(package[len(ROOT):])
            else:
                command = ['go', 'test', *mode, '-count=1', '-timeout=' + timeout, *selected]
            commands.append((group, selected, command, cwd))
        # Validate every inventory/pattern before executing any test partition.
        for group, selected, command, cwd in commands:
            print(json.dumps({'bundle': name, 'group': group, 'race': race,
                              'total_packages': len(packages), 'selected_packages': len(selected),
                              'command': command}), flush=True)
            if not list_only:
                timed_run('execute:' + group, command, cwd=cwd, check=True)
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    selection = parser.add_mutually_exclusive_group(required=True)
    selection.add_argument('--group', choices=GROUPS)
    selection.add_argument('--all', action='store_true', help='run every existing group sequentially')
    selection.add_argument('--bundle', help='run one compact mode-specific lane of existing partitions')
    parser.add_argument('--race', action='store_true')
    parser.add_argument('--list-only', action='store_true')
    args = parser.parse_args(argv)
    if args.bundle is not None:
        return run_bundle(args.bundle, args.race, args.list_only)
    result = subprocess.run(['go', 'list', './...'], text=True, stdout=subprocess.PIPE, check=True)
    packages = inventory(result.stdout)
    grouped = groups(packages)
    mode = ['-race'] if args.race else []
    commands, inventories = [], {}
    for group in GROUPS if args.all else (args.group,):
        selected = grouped[group]
        if not selected:
            raise ValueError('selected group has no packages')
        api = group in ('api-a', 'api-b')
        timeout = '12m' if args.race and api else '8m'
        command = ['go', 'test', *mode, '-count=1', '-timeout=' + timeout]
        if api or group in PARTITIONS or group == 'manager' or group in MANAGER_TESTS or group == 'cli-command' or group in CLI_CONTRIBUTION_TESTS:
            package = COMMAND if group == 'cli-command' or group in CLI_CONTRIBUTION_TESTS else API if api else MANAGER if group == 'manager' or group in MANAGER_TESTS else PARTITIONS[group][0]
            if package not in inventories:
                listing = subprocess.run([*command, '-list', '.', package], text=True, stdout=subprocess.PIPE, check=True)
                inventories[package] = test_inventory(listing.stdout, package)
            command += ['-run', test_pattern(inventories[package], group)]
        command += selected
        commands.append((group, selected, command))
    for group, selected, command in commands:
        print(json.dumps({'group': group, 'race': args.race, 'total_packages': len(packages), 'selected_packages': len(selected), 'command': command}), flush=True)
        if not args.list_only:
            subprocess.run(command, check=True)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print('Go shard denied: ' + str(error), file=sys.stderr)
        sys.exit(1)
