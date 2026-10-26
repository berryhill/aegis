#!/usr/bin/env python3
"""Run only the synthetic local cold-owner joined test using a test-only overlay.
No native/provider/GitHub proof. No production constructor or policy changes.
"""
import json, pathlib, subprocess, sys
root=pathlib.Path(__file__).resolve().parents[1]
out=root/'.acceptance'; out.mkdir(exist_ok=True)
overlay=out/'cold-overlay.json'
overlay.write_text(json.dumps({'Replace':{str(root/'internal/delivery/cold_qualification_factory.go'):str(root/'internal/app/testdata/cold_qualification_factory.go.txt')}}))
cmd=['go',sys.argv[1] if len(sys.argv)>1 else 'test','-overlay='+str(overlay),'-tags=coldqualification']
if cmd[1]=='test':cmd+=['./internal/app','-run','^TestColdJoinedPublicationOwner$','-count=1','-timeout=180s']+sys.argv[2:]
else:cmd+=['./internal/app']+sys.argv[2:]
raise SystemExit(subprocess.call(cmd,cwd=root))
