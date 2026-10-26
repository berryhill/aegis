"""Repository routing regressions plus opt-in actual fresh Hermes loader proof."""
import os
from pathlib import Path
import subprocess
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
POLICY = '.hermes/acceptance-port-isolation.md'


class ContextTests(unittest.TestCase):
    def test_essential_policy_is_direct_and_early(self):
        agents = (ROOT / 'AGENTS.md').read_text()
        early = agents[:3000]
        for expected in (POLICY, '8448', '8098', '8450–8469', 'flock', 'isolate',
                         'credentials', 'entire foreground', 'fresh effective'):
            self.assertIn(expected, early)
        self.assertFalse((ROOT / '.hermes.md').exists())
        self.assertFalse((ROOT / 'HERMES.md').exists())

    def test_all_contracts_route_without_replacing_product_criteria(self):
        for name in ('README', 'implementation-delivery-contract',
                     'contribution-happy-path-contract', 'loop-happy-path-hard-acceptance',
                     'graph-happy-path-hard-acceptance', 'trigger-happy-path-hard-acceptance'):
            text = (ROOT / '.hermes' / (name + '.md')).read_text()
            self.assertIn('acceptance-port-isolation.md', text, name)
        for name in ('CONTRIBUTING.md', 'skills/aegis/SKILL.md'):
            self.assertIn(POLICY, (ROOT / name).read_text())
        contribution = (ROOT / '.hermes/contribution-happy-path-contract.md').read_text()
        self.assertIn('same exact immutable revision/digest', contribution)
        self.assertIn('PASSED receipt and SUCCEEDED disposition', contribution)
        self.assertIn('report that gate as NOT EXERCISED', contribution)

    @unittest.skipUnless(os.environ.get('AEGIS_HERMES_SOURCE'),
                         'set AEGIS_HERMES_SOURCE for actual installed fresh-loader proof')
    def test_actual_fresh_loader_root_nested_and_bounded_context(self):
        runtime = Path(os.environ['AEGIS_HERMES_SOURCE']).resolve(strict=True)
        code = r'''
import json, pathlib, sys
runtime, root = map(pathlib.Path, sys.argv[1:])
sys.path.insert(0, str(runtime))
import agent.prompt_builder as loader
assert pathlib.Path(loader.__file__).resolve() == runtime / 'agent/prompt_builder.py'
results = []
for cwd in (root, root / 'scripts'):
    for length in (None, 8192):
        text = loader.build_context_files_prompt(cwd=str(cwd), skip_soul=True,
                                                 context_length=length)
        for expected in ('acceptance-port-isolation.md', '8448', '8098', '8450–8469',
                         'entire foreground', 'configuration/credentials/history'):
            assert expected in text, (str(cwd), length, expected)
        results.append({'cwd': str(cwd), 'context_length': length,
                        'effective_port_routing': True})
print(json.dumps({'loader': str(pathlib.Path(loader.__file__).resolve()), 'checks': results}))
'''
        result = subprocess.run([sys.executable, '-c', code, str(runtime), str(ROOT)],
                                capture_output=True, text=True, timeout=60)
        self.assertEqual(0, result.returncode, result.stderr)
        print(result.stdout.strip())


if __name__ == '__main__':
    unittest.main()
