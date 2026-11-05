import importlib.util
from pathlib import Path
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("ci_evidence", Path(__file__).with_name("verify-ci-evidence.py"))
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
SHA = "a" * 40


class EvidenceTests(unittest.TestCase):
    def fixture(self):
        run = {"id": 123, "run_attempt": 2, "workflow_id": 456,
               "head_sha": SHA, "head_branch": "main", "event": "push",
               "path": ".github/workflows/ci.yml", "status": "completed",
               "conclusion": "success", "repository": {"full_name": "berryhill/aegis"},
               "head_repository": {"full_name": "berryhill/aegis"},
               "html_url": "https://github.com/berryhill/aegis/actions/runs/123"}
        jobs = [{"id": i + 1, "run_id": 123, "head_sha": SHA, "status": "completed",
                 "conclusion": "success", "name": name} for i, name in enumerate(MODULE.REQUIRED_JOBS)]
        return run, jobs

    def test_valid_exact_source(self):
        run, jobs = self.fixture()
        self.assertEqual(MODULE.validate(run, jobs, SHA, 456)["source_revision"], SHA)

    def test_fail_closed_run_identity_and_outcomes(self):
        for key, value in [("head_sha", "b" * 40), ("head_branch", "feature"),
                           ("event", "pull_request"), ("workflow_id", 999),
                           ("path", ".github/workflows/other.yml"), ("conclusion", "failure"),
                           ("status", "in_progress"), ("run_attempt", 0), ("id", True),
                           ("repository", {"full_name": "attacker/aegis"}),
                           ("head_repository", {"full_name": "attacker/aegis"})]:
            with self.subTest(key=key):
                run, jobs = self.fixture()
                run[key] = value
                with self.assertRaises(ValueError):
                    MODULE.validate(run, jobs, SHA, 456)

    def test_all_jobs_required_and_no_hidden_failure(self):
        for mutate in [lambda j: j.pop(), lambda j: j.append(dict(j[0])),
                       lambda j: j[0].update(conclusion="skipped"),
                       lambda j: j[0].update(status="in_progress"),
                       lambda j: j[0].update(run_id=124),
                       lambda j: j[0].update(head_sha="b" * 40),
                       lambda j: j.append({"id": 999, "run_id": 123, "head_sha": SHA,
                                            "name": "extra shard", "status": "completed", "conclusion": "failure"})]:
            run, jobs = self.fixture()
            mutate(jobs)
            with self.assertRaises(ValueError):
                MODULE.validate(run, jobs, SHA, 456)

    def test_each_rebalanced_shard_is_required_in_both_modes(self):
        for mode in ('normal', 'race'):
            for group in (name.split(' / ')[1] for name in MODULE.REQUIRED_JOBS if name.startswith(f'Go {mode} / ')):
                name = f'Go {mode} / {group}'
                with self.subTest(name=name):
                    self.assertIn(name, MODULE.REQUIRED_JOBS)
                    run, jobs = self.fixture()
                    jobs = [job for job in jobs if job['name'] != name]
                    with self.assertRaisesRegex(ValueError, 'missing'):
                        MODULE.validate(run, jobs, SHA, 456)

    def test_cancelled_execution_successors_deny_even_with_green_aggregates(self):
        for mode in ('normal', 'race'):
            for group in (name.split(' / ')[1] for name in MODULE.REQUIRED_JOBS if name.startswith(f'Go {mode} / ')):
                name = f'Go {mode} / {group}'
                for status in ('cancelled', 'skipped', 'failure'):
                    with self.subTest(name=name, status=status):
                        run, jobs = self.fixture()
                        next(job for job in jobs if job['name'] == name)['conclusion'] = status
                        with self.assertRaises(ValueError):
                            MODULE.validate(run, jobs, SHA, 456)

    def test_unknown_successful_shard_and_legacy_api_are_denied(self):
        for name in ('Go race / api', 'Go normal / api-c', 'Go normal / execution', 'Go race / execution', 'unknown successful gate'):
            run, jobs = self.fixture()
            jobs.append(dict(jobs[0], id=999, name=name))
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'unknown'):
                MODULE.validate(run, jobs, SHA, 456)

    def test_latest_attempt_selected_without_falling_back_to_green(self):
        older, _ = self.fixture()
        newer = dict(older, id=124, status="in_progress", conclusion=None)
        self.assertEqual(MODULE.select_run([older, newer], SHA)["id"], 124)
        newer["head_sha"] = "b" * 40
        self.assertEqual(MODULE.select_run([older, newer], SHA)["id"], 123)
        with self.assertRaises(ValueError):
            MODULE.select_run([], SHA)

    def test_sha_validation_precedes_api(self):
        for value in ["main", "A" * 40, SHA + ";true", "", "a" * 39]:
            with self.assertRaises(ValueError):
                MODULE.validate_sha(value)

    def test_main_uses_paginated_service_readback_and_latest_jobs(self):
        run, jobs = self.fixture()
        workflow = {"id": 456, "path": MODULE.WORKFLOW, "state": "active"}
        responses = [workflow, [{"workflow_runs": [run]}, {"workflow_runs": []}],
                     run, [{"jobs": jobs[:2]}, {"jobs": jobs[2:]}], run]
        with mock.patch.object(MODULE, "api", side_effect=responses) as api, mock.patch.object(MODULE.sys, "argv", ["verify-ci-evidence.py", SHA]), mock.patch("builtins.print") as output:
            MODULE.main()
            self.assertEqual(api.call_args_list[1].args[1], True)
            self.assertIn("jobs?filter=latest", api.call_args_list[3].args[0])
            self.assertEqual(output.call_count, 1)

    def test_main_denies_attempt_or_completion_drift_and_api_failure(self):
        for drift in ("selected", "final", "unavailable"):
            run, jobs = self.fixture()
            workflow = {"id": 456, "path": MODULE.WORKFLOW, "state": "active"}
            detail = dict(run, run_attempt=3) if drift == "selected" else run
            final = dict(run, status="in_progress", conclusion=None) if drift == "final" else run
            responses = [workflow, [{"workflow_runs": [run]}], detail, [{"jobs": jobs}], final]
            if drift == "unavailable":
                responses[0] = ValueError("GitHub evidence unavailable")
            with self.subTest(drift=drift), mock.patch.object(MODULE, "api", side_effect=responses), mock.patch.object(MODULE.sys, "argv", ["verify-ci-evidence.py", SHA]), mock.patch("builtins.print") as output:
                with self.assertRaises(ValueError):
                    MODULE.main()
                output.assert_not_called()

    def test_api_failure_has_no_fallback(self):
        with mock.patch.object(MODULE.subprocess, "run", side_effect=MODULE.subprocess.CalledProcessError(1, ["gh"])) as run:
            with self.assertRaisesRegex(ValueError, "no local or older-run fallback"):
                MODULE.api("repos/berryhill/aegis/actions/workflows/ci.yml")
            self.assertEqual(run.call_count, 1)

    def test_v030_optional_outcomes_are_reported_not_rewritten(self):
        for conclusion in ('success', 'failure', 'skipped', 'cancelled', 'timed_out'):
            run, jobs = self.fixture()
            run['conclusion'] = 'failure' if conclusion != 'success' else 'success'
            optional = next(j for j in jobs if j['name'] == 'Release package and installed proof')
            optional['conclusion'] = conclusion
            receipt = MODULE.validate(run, jobs, SHA, 456, '0.3.0')
            self.assertEqual(receipt['status'], 'release_eligible')
            self.assertEqual(receipt['workflow_conclusion'], run['conclusion'])
            self.assertEqual(receipt['optional_acceptance'], [{'name': optional['name'], 'conclusion': conclusion}])
            self.assertEqual(receipt['coverage_complete'], conclusion == 'success')
            for version in (None, '0.2.33', '0.3.1', '1.3.0'):
                if conclusion != 'success':
                    with self.subTest(version=version), self.assertRaises(ValueError):
                        MODULE.validate(run, jobs, SHA, 456, version)

    def test_v030_every_required_job_still_denies_nonsuccess(self):
        for name in MODULE.REQUIRED_JOBS - {'Release package and installed proof'}:
            for conclusion in ('failure', 'skipped', 'cancelled', 'timed_out'):
                run, jobs = self.fixture()
                run['conclusion'] = 'failure'
                next(j for j in jobs if j['name'] == name)['conclusion'] = conclusion
                with self.subTest(name=name, conclusion=conclusion), self.assertRaises(ValueError):
                    MODULE.validate(run, jobs, SHA, 456, '0.3.0')

    def test_v030_optional_identity_missing_and_running_still_deny(self):
        for mutation in (lambda j: j.update(head_sha='b' * 40),
                         lambda j: j.update(run_id=124), lambda j: j.update(status='in_progress'),
                         lambda j: j.update(conclusion=None)):
            run, jobs = self.fixture()
            mutation(next(j for j in jobs if j['name'] == 'Release package and installed proof'))
            with self.assertRaises(ValueError):
                MODULE.validate(run, jobs, SHA, 456, '0.3.0')
        run, jobs = self.fixture()
        for name in MODULE.REQUIRED_JOBS:
            with self.subTest(name=name), self.assertRaises(ValueError):
                MODULE.validate(run, [j for j in jobs if j['name'] != name], SHA, 456, '0.3.0')

    def test_version_validation_precedes_api(self):
        for version in ('v0.3.0', '0.3.0-rc.1', '00.3.0', '0.3.0\n', ''):
            with mock.patch.object(MODULE, 'api') as api, mock.patch.object(MODULE.sys, 'argv', ['verify-ci-evidence.py', SHA, version]):
                with self.assertRaises(ValueError):
                    MODULE.main()
                api.assert_not_called()

    def test_v030_main_consumes_service_and_denies_final_drift(self):
        for drift in (False, True):
            run, jobs = self.fixture()
            run['conclusion'] = 'failure'
            next(j for j in jobs if j['name'] == 'Release package and installed proof')['conclusion'] = 'failure'
            workflow = {'id': 456, 'path': MODULE.WORKFLOW, 'state': 'active'}
            final = dict(run, run_attempt=3) if drift else run
            responses = [workflow, [{'workflow_runs': [run]}], run, [{'jobs': jobs}], final]
            with mock.patch.object(MODULE, 'api', side_effect=responses), mock.patch.object(MODULE.sys, 'argv', ['verify-ci-evidence.py', SHA, '0.3.0']), mock.patch('builtins.print') as output:
                if drift:
                    with self.assertRaises(ValueError):
                        MODULE.main()
                    output.assert_not_called()
                else:
                    MODULE.main()
                    self.assertIn('"status": "release_eligible"', output.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
