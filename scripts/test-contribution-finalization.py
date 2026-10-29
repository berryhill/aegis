#!/usr/bin/env python3
"""Controlled actual native/continuity/delivery/finalizer proof, no live network.
Uses the existing test-only Go overlay procedure; production origin is untouched.
"""
import json
import os
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
out = root / '.candidate/issue379/builder-v10'
out.mkdir(parents=True, exist_ok=True)
overlay = out / 'overlay.json'
overlay.write_text(json.dumps({'Replace': {str(root / 'internal/delivery/final_qualification_factory.go'): str(root / 'internal/app/testdata/final_qualification_factory.go.txt')}}))
env = dict(os.environ, GOMAXPROCS='2', GOFLAGS='-p=1', GOMEMLIMIT='512MiB')
command = ['go', 'test', '-overlay=' + str(overlay), '-tags=finalqualification', './internal/managergateway', '-run', '^TestCanonicalContributionFinalization$', '-count=1', '-timeout=8m', '-v', *sys.argv[1:]]
print(' '.join(command), flush=True)
raise SystemExit(subprocess.call(command, cwd=root, env=env))
