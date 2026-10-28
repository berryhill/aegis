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
            self.assertEqual(Counter(union), Counter(packages) + Counter({shard.API: 1, shard.ORCHESTRATION: 1, shard.IMPLEMENTATION: 1, shard.MANAGER: len(shard.MANAGER_TESTS), shard.COMMAND: len(shard.CLI_TESTS)}))
            self.assertEqual(grouped, shard.groups(packages))
            self.assertEqual(grouped['api-a'], [shard.API])
            self.assertEqual(grouped['api-b'], [shard.API])
            self.assertEqual(grouped['orchestration-contribution'], [shard.ORCHESTRATION])
            self.assertEqual(grouped['implementation-contribution'], [shard.IMPLEMENTATION])
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

    def test_joined_manager_journeys_isolate_only_the_exact_measured_parent(self):
        required = {
            'manager-contribution-joint': 'TestContributionDeliveryJointNative',
            'manager-contribution-cancel': 'TestContributionDeliveryJointNativeNonlocalCancellation',
            'manager-contribution-final': 'TestManagerJoinedContributionCompletion',
            'manager-contribution-readback': 'TestManagerTerminalContributionReadback',
        }
        names = sorted(list(shard.MANAGER_TESTS.values()) + [name + 'Future' for name in required.values()] + ['Example', 'FuzzFuture'])
        remainder = shard.test_pattern(names, 'manager')
        for group, target in required.items():
            self.assertEqual(shard.MANAGER_TESTS[group], target)
            pattern = shard.test_pattern(names, group)
            self.assertEqual([name for name in names if re.fullmatch(pattern, name)], [target])
            self.assertIsNone(re.fullmatch(remainder, target))
            self.assertIsNotNone(re.fullmatch(remainder, target + 'Future'))

    def test_implementation_exact_once_current_future_defaults_and_missing_target(self):
        for count in (0, 1, 47, 48):
            names = sorted([shard.IMPLEMENTATION_TEST, shard.IMPLEMENTATION_TEST + 'Future',
                            'TestManagedNoApplyContinuation', 'Example', 'FuzzFuture'] +
                           [f'TestFuture{i}' for i in range(count)])
            seen = []
            for group in ('implementation', 'implementation-contribution'):
                pattern = shard.test_pattern(names, group)
                selected = [name for name in names if re.fullmatch(pattern, name)]
                if group == 'implementation-contribution':
                    self.assertEqual(selected, [shard.IMPLEMENTATION_TEST])
                seen.extend(selected)
                with self.assertRaises(ValueError):
                    shard.test_pattern([name for name in names if name != shard.IMPLEMENTATION_TEST], group)
            self.assertEqual(Counter(seen), Counter(names))

    def test_implementation_missing_inventory_never_executes_in_both_modes(self):
        for race in (False, True):
            for group in ('implementation', 'implementation-contribution'):
                for listing in ('', 'TestFuture\nExample\nok\t' + shard.IMPLEMENTATION + '\t0.001s\n'):
                    results = [mock.Mock(stdout='\n'.join(self.packages())), mock.Mock(stdout=listing)]
                    with mock.patch.object(shard.subprocess, 'run', side_effect=results) as run:
                        with self.assertRaises(ValueError):
                            shard.main(['--group', group] + (['--race'] if race else []))
                        self.assertEqual(run.call_count, 2)

    def test_orchestration_missing_target_or_inventory_never_executes(self):
        for group in ('orchestration', 'orchestration-contribution'):
            for listing in ('', 'TestFuture\nExample\nok\t' + shard.ORCHESTRATION + '\t0.001s\n'):
                results = [mock.Mock(stdout='\n'.join(self.packages())), mock.Mock(stdout=listing)]
                with mock.patch.object(shard.subprocess, 'run', side_effect=results) as run:
                    with self.assertRaises(ValueError):
                        shard.main(['--group', group])
                    self.assertEqual(run.call_count, 2)

    def test_cli_exact_once_future_defaults_and_missing_targets_in_both_modes(self):
        names = sorted(list(shard.CLI_TESTS.values()) + [target + 'Future' for target in shard.CLI_TESTS.values()] + ['TestOther', 'Example', 'FuzzFuture'])
        seen = []
        for group in ('cli', *shard.CLI_TESTS):
            pattern = shard.test_pattern(names, group)
            selected = [name for name in names if re.fullmatch(pattern, name)]
            if group in shard.CLI_TESTS:
                self.assertEqual(selected, [shard.CLI_TESTS[group]])
            else:
                self.assertTrue(all(target + 'Future' in selected for target in shard.CLI_TESTS.values()))
            seen.extend(selected)
            for race in (False, True):
                for missing in shard.CLI_TESTS.values():
                    listing = '\n'.join(name for name in names if name != missing) + '\nok\t' + shard.COMMAND + '\t0.001s\n'
                    with mock.patch.object(shard.subprocess, 'run', side_effect=[mock.Mock(stdout='\n'.join(self.packages())), mock.Mock(stdout=listing)]) as run:
                        with self.assertRaises(ValueError):
                            shard.main(['--group', group] + (['--race'] if race else []))
                        self.assertEqual(run.call_count, 2)
        self.assertEqual(Counter(seen), Counter(names))
        self.assertEqual(shard.GROUPS['cli-shell'], ('cmd/aegis', 'internal/console'))

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
                cli = group == 'cli' or group in shard.CLI_TESTS
                partition = api or group in shard.PARTITIONS or group.startswith('manager') or cli
                package, target = shard.PARTITIONS.get(group, (shard.API, ''))
                group_listing = listing if api else listing.replace(shard.API, package).replace('TestOne\n', target + '\nTestOne\n')
                if group.startswith('manager'):
                    package = shard.MANAGER
                    group_listing = '\n'.join(list(shard.MANAGER_TESTS.values()) + ['TestFuture', 'Example']) + '\nok\t' + package + '\t0.001s\n'
                if cli:
                    package = shard.COMMAND
                    group_listing = '\n'.join(list(shard.CLI_TESTS.values()) + ['TestFuture', 'Example']) + '\nok\t' + package + '\t0.001s\n'
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

                    if partition:
                        inventory = run.call_args_list[1].args[0]
                        self.assertEqual('-race' in inventory, race)
                        self.assertIn('-list', inventory)
                        pattern = command[command.index('-run') + 1]
                        names = shard.test_inventory(group_listing, package)
                        selected = names[0 if group == 'api-a' else 1::2] if api else [name for name in names if (name == target) == group.endswith('-contribution')]
                        if group.startswith('manager'):
                            selected = [shard.MANAGER_TESTS[group]] if group in shard.MANAGER_TESTS else [name for name in names if name not in shard.MANAGER_TESTS.values()]
                        if cli:
                            selected = [shard.CLI_TESTS[group]] if group in shard.CLI_TESTS else [name for name in names if name not in shard.CLI_TESTS.values()]
                        if group.startswith('implementation'):
                            selected = [name for name in names if (name == shard.IMPLEMENTATION_TEST) == (group == 'implementation-contribution')]
                        self.assertEqual(inventory[-1], package)
                        for name in names:
                            self.assertEqual(bool(re.search(pattern, name)), name in selected)
                            self.assertIsNone(re.search(pattern, name + 'Suffix'))

    def test_implementation_exact_once_and_missing_target(self):
        target = 'TestManagedContributionRepairAndCustody'
        for race in (False, True):
            names = sorted([target, target + 'Future', 'TestFuture', 'Example', 'FuzzSeed'])
            seen = []
            for group in ('implementation', 'implementation-contribution'):
                pattern = shard.test_pattern(names, group)
                selected = [name for name in names if re.fullmatch(pattern, name)]
                self.assertEqual(selected, [target] if group.endswith('-contribution') else [n for n in names if n != target])
                self.assertIsNone(re.search(pattern, target + 'Suffix'))
                seen.extend(selected)
                with self.assertRaises(ValueError):
                    shard.test_pattern(['TestFuture', 'Example'], group)
            self.assertEqual(Counter(seen), Counter(names))

    def all_listing(self, package):
        names = ['TestFuture', 'TestFuturePrefix', 'Example', 'FuzzSeed', 'BenchmarkUnused']
        if package == shard.ORCHESTRATION:
            names.append(shard.CONTRIBUTION_TEST)
        if package == shard.IMPLEMENTATION:
            names.append(shard.IMPLEMENTATION_CONTRIBUTION_TEST)
        if package == shard.MANAGER:
            names.extend(shard.MANAGER_TESTS.values())
        if package == shard.COMMAND:
            names.extend(shard.CLI_TESTS.values())
        return '\n'.join(names) + '\nok\t' + package + '\t0.001s\n'

    def test_all_modes_execute_exact_existing_groups_and_defaults(self):
        for race in (False, True):
            for list_only in (False, True):
                def respond(command, **kwargs):
                    if command[:2] == ['go', 'list']:
                        return mock.Mock(stdout='\n'.join(self.packages()))
                    if '-list' in command:
                        return mock.Mock(stdout=self.all_listing(command[-1]))
                    return mock.Mock()
                with mock.patch.object(shard.subprocess, 'run', side_effect=respond) as run, mock.patch('builtins.print') as output:
                    self.assertEqual(shard.main(['--all'] + (['--race'] if race else []) + (['--list-only'] if list_only else [])), 0)
                plans = [json.loads(call.args[0]) for call in output.call_args_list]
                self.assertEqual([plan['group'] for plan in plans], list(shard.GROUPS))
                inventories = [call.args[0] for call in run.call_args_list if '-list' in call.args[0]]
                self.assertEqual(Counter(command[-1] for command in inventories), Counter([shard.API, shard.ORCHESTRATION, shard.IMPLEMENTATION, shard.MANAGER, shard.COMMAND]))
                executed = [call.args[0] for call in run.call_args_list if call.args[0][:2] == ['go', 'test'] and '-list' not in call.args[0]]
                self.assertEqual(executed, [] if list_only else [p['command'] for p in plans])
                seen_packages = []
                seen_defaults = {package: [] for package in (shard.API, shard.ORCHESTRATION, shard.IMPLEMENTATION, shard.MANAGER, shard.COMMAND)}
                for plan in plans:
                    command = plan['command']
                    self.assertEqual('-race' in command, race)
                    self.assertIn('-count=1', command)
                    self.assertIn('-timeout=12m' if race and plan['group'].startswith('api-') else '-timeout=8m', command)
                    packages = [arg for arg in command if arg.startswith(shard.ROOT)]
                    seen_packages.extend(packages)
                    if '-run' in command:
                        package = packages[0]
                        pattern = command[command.index('-run') + 1]
                        names = shard.test_inventory(self.all_listing(package), package)
                        seen_defaults[package].extend(name for name in names if re.fullmatch(pattern, name))
                self.assertEqual(Counter(seen_packages), Counter(self.packages()) + Counter({shard.API: 1, shard.ORCHESTRATION: 1, shard.IMPLEMENTATION: 1, shard.MANAGER: len(shard.MANAGER_TESTS), shard.COMMAND: len(shard.CLI_TESTS)}))
                for package, names in seen_defaults.items():
                    self.assertEqual(Counter(names), Counter(shard.test_inventory(self.all_listing(package), package)))

    def test_all_missing_partition_fails_before_any_execution(self):
        for missing in (shard.CONTRIBUTION_TEST, shard.IMPLEMENTATION_CONTRIBUTION_TEST, *shard.MANAGER_TESTS.values(), *shard.CLI_TESTS.values()):
            def respond(command, **kwargs):
                if command[:2] == ['go', 'list']:
                    return mock.Mock(stdout='\n'.join(self.packages()))
                self.assertIn('-list', command, 'executed before complete partition validation')
                return mock.Mock(stdout=self.all_listing(command[-1]).replace(missing + '\n', ''))
            with mock.patch.object(shard.subprocess, 'run', side_effect=respond), self.assertRaises(ValueError):
                shard.main(['--all'])

    def test_selection_is_required_and_mutually_exclusive(self):
        for args in ([], ['--all', '--group', 'api-a']):
            with mock.patch.object(shard.subprocess, 'run') as run, mock.patch('sys.stderr'), self.assertRaises(SystemExit):
                shard.main(args)
            run.assert_not_called()

    def test_all_stops_on_failed_group_without_running_later_groups(self):
        executed = []
        def respond(command, **kwargs):
            if command[:2] == ['go', 'list']:
                return mock.Mock(stdout='\n'.join(self.packages()))
            if '-list' in command:
                return mock.Mock(stdout=self.all_listing(command[-1]))
            executed.append(command)
            raise subprocess.CalledProcessError(1, command)
        with mock.patch.object(shard.subprocess, 'run', side_effect=respond), mock.patch('builtins.print'), self.assertRaises(subprocess.CalledProcessError):
            shard.main(['--all'])
        self.assertEqual(len(executed), 1)

    def test_invalid_group_rejected_before_go(self):
        with mock.patch.object(shard.subprocess, 'run') as run, mock.patch('sys.stderr'):
            with self.assertRaises(SystemExit):
                shard.main(['--group', 'unknown'])
            run.assert_not_called()

    def test_implementation_missing_target_never_executes_both_modes(self):
        for race in (False, True):
            for group in ('implementation', 'implementation-contribution'):
                for listing in ('', 'TestFuture\nExample\nok\t' + shard.IMPLEMENTATION + '\t0.001s\n'):
                    results = [mock.Mock(stdout='\n'.join(self.packages())), mock.Mock(stdout=listing)]
                    with mock.patch.object(shard.subprocess, 'run', side_effect=results) as run:
                        with self.assertRaises(ValueError):
                            shard.main(['--group', group] + (['--race'] if race else []))
                        self.assertEqual(run.call_count, 2)

    def test_parent_patterns_are_escaped_and_anchored(self):
        target = 'TestManagedContributionRepairAndCustody'
        names = [target, 'TestLiteral.Dot', 'TestLiteral+Plus']
        pattern = shard.test_pattern(names, 'implementation')
        for name in names[1:]:
            self.assertIsNotNone(re.fullmatch(pattern, name))
            self.assertIsNone(re.search(pattern, 'prefix' + name))
            self.assertIsNone(re.search(pattern, name + 'suffix'))
        self.assertIsNone(re.search(pattern, 'TestLiteralXDot'))
        self.assertIsNone(re.search(pattern, 'TestLiterallPlus'))

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
func TestManagedContributionRepairAndCustody(t *testing.T) { ready(t); t.Run("repair", func(t *testing.T) { ready(t) }) }
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
                self.assertCountEqual(names, ['TestOne', 'TestOnePrefix', 'TestFuture', shard.CONTRIBUTION_TEST, shard.IMPLEMENTATION_CONTRIBUTION_TEST, 'Example', 'FuzzSeed'])
                for partitions in (('api-a', 'api-b'), ('orchestration', 'orchestration-contribution'), ('implementation', 'implementation-contribution')):
                    seen = []
                    for group in partitions:
                        result = subprocess.run([*base, '-json', '-run', shard.test_pattern(names, group), pkg], cwd=root, capture_output=True, text=True, check=True)
                        seen += [e['Test'] for line in result.stdout.splitlines() if (e := json.loads(line)).get('Action') == 'run' and 'Test' in e]
                    self.assertEqual(Counter(seen), Counter(names + ['TestOne/child', 'FuzzSeed/seed#0', shard.CONTRIBUTION_TEST + '/handoff', shard.IMPLEMENTATION_CONTRIBUTION_TEST + '/repair']))


if __name__ == '__main__':
    unittest.main()
