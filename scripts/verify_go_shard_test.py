"""Exact package coverage and real Go fixture regression tests."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('shard', Path(__file__).with_name('verify-go-shard.py'))
assert spec and spec.loader
shard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shard)


class ShardTests(unittest.TestCase):
    def packages(self):
        return [shard.ROOT + p for paths in shard.GROUPS.values() for p in paths] + [shard.ROOT + 'new', shard.ROOT + 'empty']

    def test_exact_once_union_including_new_and_zero_test_packages(self):
        packages = self.packages()
        grouped = shard.groups(packages)
        union = [p for group in grouped.values() for p in group]
        self.assertCountEqual(union, packages)
        self.assertEqual(len(union), len(set(union)))
        self.assertEqual(grouped['remainder-a'], [shard.ROOT + 'empty'])
        self.assertEqual(grouped['remainder-b'], [shard.ROOT + 'new'])
        self.assertEqual(grouped, shard.groups(list(reversed(packages))))

    def test_odd_remainder_is_sorted_alternating_and_exact_once(self):
        named = [shard.ROOT + p for paths in shard.GROUPS.values() for p in paths]
        remainder = [shard.ROOT + f'new-{i:02d}' for i in range(47)]
        grouped = shard.groups(list(reversed(named + remainder)))
        self.assertEqual(grouped['remainder-a'], remainder[::2])
        self.assertEqual(grouped['remainder-b'], remainder[1::2])
        self.assertEqual([len(grouped[n]) for n in ('remainder-a', 'remainder-b')], [24, 23])
        union = [p for group in grouped.values() for p in group]
        self.assertCountEqual(union, named + remainder)
        self.assertEqual(len(union), len(set(union)))

    def test_measured_slow_update_is_isolated_from_both_remainders(self):
        self.assertEqual(shard.GROUPS['update'], ('internal/update',))
        # Inventory growth/reordering must not put the measured 368-373s package
        # back beside cumulative remainder costs in either mode.
        for count in (0, 1, 46, 47, 48):
            packages = self.packages() + [shard.ROOT + f'future-{i}' for i in range(count)]
            grouped = shard.groups(list(reversed(packages)))
            self.assertEqual(grouped['update'], [shard.ROOT + 'internal/update'])
            for name in ('remainder-a', 'remainder-b'):
                self.assertNotIn(shard.ROOT + 'internal/update', grouped[name])
            union = [p for group in grouped.values() for p in group]
            self.assertCountEqual(union, packages)
            self.assertEqual(len(union), len(set(union)))

    def test_empty_unknown_malformed_and_duplicate_listing_fail(self):
        for text in ['', '\n', '{}', 'other/module', shard.ROOT, shard.ROOT + 'bad name', '\n'.join([shard.ROOT + 'p'] * 2)]:
            with self.subTest(text=text), self.assertRaises(ValueError):
                shard.inventory(text)

    def test_missing_named_package_fails(self):
        with self.assertRaisesRegex(ValueError, 'missing'):
            shard.groups(self.packages()[1:])

    def test_overlapping_group_fails(self):
        packages = self.packages()
        with mock.patch.dict(shard.GROUPS, {'overlap': ('internal/api',)}):
            with self.assertRaisesRegex(ValueError, 'more than once'):
                shard.groups(packages)

    def test_go_failure_never_runs_tests(self):
        with mock.patch.object(shard.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, ['go'])) as run:
            with self.assertRaises(subprocess.CalledProcessError):
                shard.main(['--group', 'api'])
            self.assertEqual(run.call_count, 1)

    def test_every_mode_has_explicit_uncached_complete_package_command(self):
        for race in (False, True):
            for group in shard.GROUPS:
                with mock.patch.object(shard.subprocess, 'run', return_value=mock.Mock(stdout='\n'.join(self.packages()))) as run, mock.patch('builtins.print'):
                    shard.main(['--group', group] + (['--race'] if race else []))
                    command = run.call_args.args[0]
                    self.assertEqual('-race' in command, race)
                    self.assertIn('-count=1', command)
                    self.assertNotIn('-run', command)
                    timeout = '-timeout=12m' if race and group == 'api' else '-timeout=8m'
                    self.assertEqual(command[command.index(timeout)+1:], shard.groups(self.packages())[group])

    def test_invalid_group_rejected_before_go(self):
        with mock.patch.object(shard.subprocess, 'run') as run, mock.patch('sys.stderr'):
            with self.assertRaises(SystemExit):
                shard.main(['--group', 'unknown'])
            run.assert_not_called()

    def test_outer_race_jobs_allow_setup_and_complete_slow_packages(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/ci.yml').read_text()
        job = workflow.split('  go-shards:\n', 1)[1].split('  installed-proof:', 1)[0]
        allowance = "${{ matrix.mode == 'race' && (matrix.group == 'api' || matrix.group == 'remainder-a') && 16 || 10 }}"
        self.assertIn('    timeout-minutes: ' + allowance, job)
        self.assertIn('fail-fast: false', job)
        self.assertIn('python3 scripts/verify-budget.py python3 scripts/verify-go-shard.py --group "$SHARD_GROUP" --race', job)

    def test_real_go_fixture_preserves_tests_examples_and_fuzz_seeds(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            root = Path(directory)
            (root / 'go.mod').write_text('module github.com/berryhill/aegis\n\ngo 1.26.10\n')
            package = root / 'fixture'
            package.mkdir()
            (package / 'fixture_test.go').write_text('''package fixture
import ("testing"; "fmt")
func TestOne(t *testing.T) { t.Run("child", func(t *testing.T) {}) }
func Example() { fmt.Println("example"); // Output: example
}
func FuzzSeed(f *testing.F) { f.Add("seed"); f.Fuzz(func(t *testing.T, s string) {}) }
''')
            result = subprocess.run(['go', 'list', './...'], cwd=root, capture_output=True, text=True, check=True)
            packages = shard.inventory(result.stdout)
            with mock.patch.object(shard, 'GROUPS', {'fixture': ('fixture',), 'remainder-a': (), 'remainder-b': ()}):
                self.assertEqual(shard.groups(packages)['fixture'], packages)
            result = subprocess.run(['go', 'test', '-count=1', '-json', *packages], cwd=root, capture_output=True, text=True, check=True)
            seen = [event['Test'] for line in result.stdout.splitlines() if (event := json.loads(line)).get('Action') == 'run' and '/' not in event.get('Test', '')]
            self.assertCountEqual(seen, ['TestOne', 'Example', 'FuzzSeed'])


if __name__ == '__main__':
    unittest.main()
