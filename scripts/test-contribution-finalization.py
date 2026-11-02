#!/usr/bin/env python3
"""Controlled native/continuity/delivery/finalizer proof; no live provider/remote.
The existing test-only Go overlay leaves production transport/auth unchanged.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

CASES = (
    'TestCanonicalContributionFinalization',
    'TestCanonicalContributionCIRepairFinalization',
    'TestCanonicalContributionAdvancingBaseFinalization',
)
DEFAULT_RUN = '^(' + '|'.join(CASES) + ')$'


def selected_cases(output, selection, required=()):
    """Resolve compiled roots, rejecting empty or incomplete qualification."""
    names = [line for line in output.splitlines() if line.startswith('Test')]
    if not names or len(names) != len(set(names)) or any(not re.search(selection, name) for name in names):
        raise ValueError('invalid or empty compiled finalization selection')
    if required and set(names) != set(required):
        raise ValueError('missing compiled canonical finalization case')
    return names


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('-run', default=DEFAULT_RUN)
    args = parser.parse_args(argv)
    root = Path(__file__).resolve().parents[1]
    out = root / '.candidate/issue379/builder-v10'
    out.mkdir(parents=True, exist_ok=True)
    overlay = out / 'overlay.json'
    replacements = {str(root / 'internal/delivery/final_qualification_factory.go'): str(root / 'internal/app/testdata/final_qualification_factory.go.txt')}
    # Controlled HTTP/custody substitution ONLY. Native repair, canonical
    # history and final verification run unchanged; this is not live proof.
    controller = root / 'internal/app/contribution_delivery_controller.go'
    source = controller.read_text()
    start = source.index('\t\t\tcustody, e := delivery.NewCredentialCustody(')
    end = source.index('\n\t\t}\n\t\t// A synchronous', start)
    source = source[:start] + '\t\t\tgh, e = delivery.FinalQualificationControllerGitHub(plan)\n\t\t\tif e != nil { return e }' + source[end:]
    patched = out / 'contribution_delivery_controller.go'
    patched.write_text(source)
    replacements[str(controller)] = str(patched)
    overlay.write_text(json.dumps({'Replace': replacements}))
    env = dict(os.environ, GOMAXPROCS='2', GOFLAGS='-p=1', GOMEMLIMIT='512MiB')
    base = ['go', 'test', '-overlay=' + str(overlay), '-tags=finalqualification', './internal/managergateway']
    inventory = subprocess.run([*base, '-list', args.run], cwd=root, env=env, text=True, capture_output=True)
    if inventory.returncode:
        print(inventory.stdout + inventory.stderr, flush=True)
        return inventory.returncode
    try:
        names = selected_cases(inventory.stdout, args.run, CASES if args.run == DEFAULT_RUN else ())
    except ValueError as error:
        print(str(error), flush=True)
        return 1
    print('compiled finalization selection: ' + ', '.join(names), flush=True)
    command = [*base, '-run', args.run, '-count=1', '-timeout=8m', '-v']
    print(' '.join(command), flush=True)
    return subprocess.call(command, cwd=root, env=env)


if __name__ == '__main__':
    raise SystemExit(main())
