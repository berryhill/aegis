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
            for group in ('api-a', 'api-b', 'implementation', 'implementation-contribution', 'orchestration', 'orchestration-contribution', 'manager', 'manager-contribution-repair', 'manager-contribution-disconnect', 'manager-contribution-resume', 'manager-contribution-restart', 'update', 'remainder-a', 'remainder-b'):
                name = f'Go {mode} / {group}'
                with self.subTest(name=name):
                    self.assertIn(name, MODULE.REQUIRED_JOBS)
                    run, jobs = self.fixture()
                    jobs = [job for job in jobs if job['name'] != name]
                    with self.assertRaisesRegex(ValueError, 'missing'):
                        MODULE.validate(run, jobs, SHA, 456)

    def test_cancelled_execution_successors_deny_even_with_green_aggregates(self):
        for mode in ('normal', 'race'):
            for group in ('implementation', 'implementation-contribution', 'orchestration', 'orchestration-contribution', 'manager', 'manager-contribution-repair', 'manager-contribution-disconnect', 'manager-contribution-resume', 'manager-contribution-restart'):
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


if __name__ == "__main__":
    unittest.main()
