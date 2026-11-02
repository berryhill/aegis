"""Fail-closed compiled inventory for the controlled finalqualification runner."""
import importlib.util
from pathlib import Path
import unittest

PATH = Path(__file__).with_name('test-contribution-finalization.py')
spec = importlib.util.spec_from_file_location('finalization_runner', PATH)
assert spec is not None and spec.loader is not None
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class FinalizationSelectionTest(unittest.TestCase):
    def test_default_resolves_all_three_exact_roots(self):
        self.assertEqual(runner.CASES, (
            'TestCanonicalContributionFinalization',
            'TestCanonicalContributionCIRepairFinalization',
            'TestCanonicalContributionAdvancingBaseFinalization',
        ))
        output = '\n'.join(runner.CASES) + '\nok fixture 0.01s\n'
        self.assertEqual(runner.selected_cases(output, runner.DEFAULT_RUN, runner.CASES), list(runner.CASES))
        for name in runner.CASES:
            self.assertRegex(name, runner.DEFAULT_RUN)
        self.assertNotRegex(runner.CASES[0] + 'UnregisteredSuffix', runner.DEFAULT_RUN)

    def test_missing_empty_duplicate_foreign_roots_deny(self):
        for names in [(), runner.CASES[:-1], (*runner.CASES, runner.CASES[0]), (*runner.CASES, 'TestForeign')]:
            with self.subTest(names=names), self.assertRaises(ValueError):
                runner.selected_cases('\n'.join(names), runner.DEFAULT_RUN, runner.CASES)

    def test_explicit_selection_must_resolve(self):
        self.assertEqual(runner.selected_cases(runner.CASES[2], '^' + runner.CASES[2] + '$'), [runner.CASES[2]])
        with self.assertRaises(ValueError):
            runner.selected_cases('ok fixture [no tests to run]', '^TestMissing$')

    def test_existing_job_registers_runner_without_budget_expansion(self):
        workflow = PATH.parents[1].joinpath('.github/workflows/ci.yml').read_text()
        repository_job = workflow.split('  repository-checks:', 1)[1].split('\n  go-shards:', 1)[0]
        vet_job = workflow.split('\n  vet:', 1)[1].split('\n  authority-denial-matrix:', 1)[0]
        self.assertNotIn('test-contribution-finalization.py', repository_job)
        self.assertIn('timeout-minutes: 10', vet_job)
        self.assertIn('python3 scripts/verify-budget.py go vet ./...', vet_job)
        self.assertIn('python3 scripts/verify-budget.py python3 scripts/acceptance_port.py --pool', vet_job)
        self.assertIn('-- python3 scripts/test-contribution-finalization.py\n', vet_job)
        self.assertIn('range(8450, 8470)', vet_job)
        self.assertIn('scripts/test_contribution_finalization_test.py', vet_job)
        self.assertEqual(workflow.count('-- python3 scripts/test-contribution-finalization.py'), 1)
        # The CI invocation has no -run override: it must compile/list and then
        # execute the same fail-closed three-root default under the existing alarm.
        source = PATH.read_text()
        self.assertIn("parser.add_argument('-run', default=DEFAULT_RUN)", source)
        self.assertIn("'-list', args.run", source)
        self.assertIn('CASES if args.run == DEFAULT_RUN else ()', source)
        self.assertIn("'-run', args.run, '-count=1', '-timeout=8m'", source)


if __name__ == '__main__':
    unittest.main()
