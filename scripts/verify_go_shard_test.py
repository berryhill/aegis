"""Exact package/default-test coverage, including a real normal/race Go binary."""
import importlib.util
import json
from collections import Counter
from pathlib import Path
import re
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
        return sorted({shard.ROOT + p for paths in shard.GROUPS.values() for p in paths}) + [shard.ROOT + 'new', shard.ROOT + 'empty']

    def test_exact_once_packages_except_dynamic_partitions_including_future_packages(self):
        for count in (0, 1, 46, 47, 48):
            packages = self.packages() + [shard.ROOT + f'future-{i:02d}' for i in range(count)]
            grouped = shard.groups(list(reversed(packages)))
            union = [p for group in grouped.values() for p in group]
            self.assertEqual(Counter(union), Counter(packages) + Counter({shard.API: 1, shard.ORCHESTRATION: 1, shard.MANAGER: 3}))
            self.assertEqual(grouped, shard.groups(packages))
            self.assertEqual(grouped['api-a'], [shard.API])
            self.assertEqual(grouped['api-b'], [shard.API])
            self.assertEqual(grouped['orchestration-contribution'], [shard.ORCHESTRATION])
            self.assertEqual(grouped['update'], [shard.ROOT + 'internal/update'])
            for heavy in ('implementation', 'orchestration'):
                self.assertEqual(grouped[heavy], [shard.ROOT + 'internal/' + heavy])
            remainder = sorted(p for p in packages if p not in {shard.ROOT + p for paths in shard.GROUPS.values() for p in paths})
            self.assertEqual(grouped['remainder-a'], remainder[::2])
            self.assertEqual(grouped['remainder-b'], remainder[1::2])

    def test_invalid_package_inventory_and_overlap_fail(self):
        for text in ['', '\n', '{}', 'other/module', shard.ROOT, shard.ROOT + 'bad name', '\n'.join([shard.ROOT + 'p'] * 2)]:
            with self.subTest(text=text), self.assertRaises(ValueError):
                shard.inventory(text)
        with self.assertRaises(ValueError):
            shard.groups([p for p in self.packages() if p != shard.API])
        for duplicate in ('internal/api', 'internal/update', 'internal/implementation', 'internal/orchestration'):
            with mock.patch.dict(shard.GROUPS, {'overlap': (duplicate,)}), self.assertRaises(ValueError):
                shard.groups(self.packages())

    def test_invalid_test_inventory_fails_closed(self):
        footer = '\nok\t' + shard.API + '\t0.001s\n'
        for text in ['', 'TestOne\nTestTwo\n', footer, 'TestOne\nTestOne' + footer,
                     'TestOne\nUnknown' + footer, 'TestOne\nTestTwo/child' + footer,
                     'BenchmarkOnly' + footer, 'TestOne\nTestTwo' + footer + 'noise\n']:
            with self.subTest(text=text), self.assertRaises(ValueError):
                shard.test_inventory(text)

    def test_orchestration_exact_once_current_and_future_defaults(self):
        for count in (0, 1, 47, 48):
            names = sorted([shard.CONTRIBUTION_TEST, 'TestImplementationQueueNativeCompletion',
                            shard.CONTRIBUTION_TEST + 'Future', 'Example', 'FuzzFuture'] +
                           [f'TestFuture{i}' for i in range(count)])
            seen = []
            for group in ('orchestration', 'orchestration-contribution'):
                pattern = shard.test_pattern(names, group)
                selected = [name for name in names if re.fullmatch(pattern, name)]
                if group == 'orchestration-contribution':
                    self.assertEqual(selected, [shard.CONTRIBUTION_TEST])
                seen.extend(selected)
            self.assertEqual(Counter(seen), Counter(names))

    def test_manager_exact_once_current_future_and_missing_heavy(self):
        for count in (0, 1, 47, 48):
            names = sorted(list(shard.MANAGER_TESTS.values()) + ['Example', 'FuzzFuture', 'TestContributionManagerApprovedNativeRepairFuture'] + [f'TestFuture{i}' for i in range(count)])
            seen = []
            for group in ('manager', *shard.MANAGER_TESTS):
                pattern = shard.test_pattern(names, group)
                seen.extend(name for name in names if re.fullmatch(pattern, name))
                for missing in shard.MANAGER_TESTS.values():
                    with self.assertRaises(ValueError):
                        shard.test_pattern([name for name in names if name != missing], group)
            self.assertEqual(Counter(seen), Counter(names))

    def test_orchestration_missing_target_or_inventory_never_executes(self):
        for group in ('orchestration', 'orchestration-contribution'):
            for listing in ('', 'TestFuture\nExample\nok\t' + shard.ORCHESTRATION + '\t0.001s\n'):
                results = [mock.Mock(stdout='\n'.join(self.packages())), mock.Mock(stdout=listing)]
                with mock.patch.object(shard.subprocess, 'run', side_effect=results) as run:
                    with self.assertRaises(ValueError):
                        shard.main(['--group', group])
                    self.assertEqual(run.call_count, 2)

    def test_inventory_failure_never_executes_partition(self):
        for failure in (subprocess.CalledProcessError(1, ['go']), mock.Mock(stdout='')):
            with mock.patch.object(shard.subprocess, 'run', side_effect=[mock.Mock(stdout='\n'.join(self.packages())), failure]) as run:
                with self.assertRaises((ValueError, subprocess.CalledProcessError)):
                    shard.main(['--group', 'api-a'])
                self.assertEqual(run.call_count, 2)

    def test_every_mode_command_and_matching_inventory_mode(self):
        listing = 'TestOne\nTestOnePrefix\nExample\nFuzzSeed\nBenchmarkUnused\nok\t' + shard.API + '\t0.001s\n'
        for race in (False, True):
            for group in shard.GROUPS:
                api = group.startswith('api-')
                partition = api or group.startswith(('orchestration', 'manager'))
                package = shard.API if api else shard.MANAGER if group.startswith("manager") else shard.ORCHESTRATION
                group_listing = listing if api else listing.replace(shard.API, package).replace('TestOne\n', shard.CONTRIBUTION_TEST + '\nTestOne\n')
                if group.startswith('manager'):
                    group_listing = '\n'.join(list(shard.MANAGER_TESTS.values()) + ['TestFuture', 'Example']) + '\nok\t' + package + '\t0.001s\n'
                results = [mock.Mock(stdout='\n'.join(self.packages()))]
                if partition:
                    results.append(mock.Mock(stdout=group_listing))
                results.append(mock.Mock())
                with mock.patch.object(shard.subprocess, 'run', side_effect=results) as run, mock.patch('builtins.print'):
                    shard.main(['--group', group] + (['--race'] if race else []))
                    command = run.call_args.args[0]
                    self.assertEqual('-race' in command, race)
                    self.assertIn('-count=1', command)
                    self.assertIn('-timeout=12m' if race and api else '-timeout=8m', command)
                    self.assertEqual('-run' in command, partition)
                    if group == 'implementation':
                        self.assertEqual(command, ['go', 'test', *(['-race'] if race else []),
                                                  '-count=1', '-timeout=8m', shard.ROOT + 'internal/' + group])
                    if partition:
                        inventory = run.call_args_list[1].args[0]
                        self.assertEqual('-race' in inventory, race)
                        self.assertIn('-list', inventory)
                        pattern = command[command.index('-run') + 1]
                        names = shard.test_inventory(group_listing, package)
                        selected = names[0 if group == 'api-a' else 1::2] if api else [name for name in names if (name == shard.CONTRIBUTION_TEST) == (group == 'orchestration-contribution')]
                        if group.startswith('manager'):
                            selected = [shard.MANAGER_TESTS[group]] if group in shard.MANAGER_TESTS else [name for name in names if name not in shard.MANAGER_TESTS.values()]
                        self.assertEqual(inventory[-1], package)
                        for name in names:
                            self.assertEqual(bool(re.search(pattern, name)), name in selected)
                            self.assertIsNone(re.search(pattern, name + 'Suffix'))

    def test_invalid_group_rejected_before_go(self):
        with mock.patch.object(shard.subprocess, 'run') as run, mock.patch('sys.stderr'):
            with self.assertRaises(SystemExit):
                shard.main(['--group', 'unknown'])
            run.assert_not_called()

    def test_real_go_fixture_preserves_every_default_leaf_and_testmain(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            root = Path(directory)
            (root / 'go.mod').write_text('module github.com/berryhill/aegis\n\ngo 1.26.10\n')
            package = root / 'fixture'
            package.mkdir()
            (package / 'fixture_test.go').write_text('''package fixture
import ("testing"; "fmt"; "os")
var mainReady bool
func TestMain(m *testing.M) { mainReady = true; os.Exit(m.Run()) }
func ready(t *testing.T) { if !mainReady { t.Fatal("TestMain did not run") } }
func TestOne(t *testing.T) { ready(t); t.Run("child", func(t *testing.T) { ready(t) }) }
func TestOnePrefix(t *testing.T) { ready(t) }
func TestFuture(t *testing.T) { ready(t) }
func TestContributionQueueNativeRepairIntegration(t *testing.T) { ready(t); t.Run("handoff", func(t *testing.T) { ready(t) }) }
func Example() { if !mainReady { panic("TestMain did not run") }; fmt.Println("example"); // Output: example
}
func FuzzSeed(f *testing.F) { if !mainReady { f.Fatal("TestMain did not run") }; f.Add("seed"); f.Fuzz(func(t *testing.T, s string) { ready(t) }) }
func BenchmarkUnused(b *testing.B) { b.Fatal("benchmarks are not default tests") }
''')
            pkg = shard.ROOT + 'fixture'
            for race in (False, True):
                base = ['go', 'test', *(['-race'] if race else []), '-count=1', '-timeout=8m']
                listing = subprocess.run([*base, '-list', '.', pkg], cwd=root, capture_output=True, text=True, check=True)
                names = shard.test_inventory(listing.stdout, pkg)
                self.assertCountEqual(names, ['TestOne', 'TestOnePrefix', 'TestFuture', shard.CONTRIBUTION_TEST, 'Example', 'FuzzSeed'])
                for partitions in (('api-a', 'api-b'), ('orchestration', 'orchestration-contribution')):
                    seen = []
                    for group in partitions:
                        result = subprocess.run([*base, '-json', '-run', shard.test_pattern(names, group), pkg], cwd=root, capture_output=True, text=True, check=True)
                        seen += [e['Test'] for line in result.stdout.splitlines() if (e := json.loads(line)).get('Action') == 'run' and 'Test' in e]
                    self.assertEqual(Counter(seen), Counter(names + ['TestOne/child', 'FuzzSeed/seed#0', shard.CONTRIBUTION_TEST + '/handoff']))


if __name__ == '__main__':
    unittest.main()
