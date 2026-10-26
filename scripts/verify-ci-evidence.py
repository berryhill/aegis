#!/usr/bin/env python3
"""Require GitHub-owned successful CI for one exact canonical main revision.

This consumes service readback, never a caller-supplied verdict or local receipt.
The release child may differ only by its separately verified changelog transform.
"""
import json
import re
import subprocess
import sys

REPOSITORY = "berryhill/aegis"
WORKFLOW = ".github/workflows/ci.yml"
REQUIRED_JOBS = frozenset({"Exact-head release readiness", "Race tests", "Repository checks", "Go vet",
                           "Authority denial matrix", "Vulnerability scan",
                           "Release package and installed proof"}
                          | {f"Go {mode} / {group}" for mode in ("normal", "race")
                             for group in ("api-a", "api-b", "implementation", "orchestration", "orchestration-contribution", "manager", "manager-contribution-repair", "manager-contribution-disconnect", "manager-contribution-resume", "manager-contribution-restart", "cli", "persistence", "update", "remainder-a", "remainder-b")})


def validate_sha(value):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{40}", value):
        raise ValueError("source revision must be one full lowercase commit digest")
    return value


def positive(value):
    return type(value) is int and value > 0


def select_run(runs, revision):
    matching = [run for run in runs if run.get("head_sha") == revision
                and run.get("event") == "push" and run.get("head_branch") == "main"]
    if not matching or any(not positive(run.get("id")) for run in matching):
        raise ValueError("no unambiguous canonical main CI run for exact source; wait for CI, do not release")
    return max(matching, key=lambda run: run["id"])


def validate(run, jobs, revision, workflow_id):
    validate_sha(revision)
    if (not positive(run.get("id")) or not positive(run.get("run_attempt"))
            or run.get("workflow_id") != workflow_id or run.get("path") != WORKFLOW
            or run.get("head_sha") != revision or run.get("head_branch") != "main"
            or run.get("event") != "push"
            or run.get("repository", {}).get("full_name") != REPOSITORY
            or run.get("head_repository", {}).get("full_name") != REPOSITORY
            or run.get("status") != "completed" or run.get("conclusion") != "success"):
        raise ValueError("latest exact-source canonical CI is not complete and successful")
    names, ids = set(), set()
    if not jobs:
        raise ValueError("CI job evidence is absent")
    for job in jobs:
        name, ident = job.get("name"), job.get("id")
        if (not isinstance(name, str) or not name or name in names
                or not positive(ident) or ident in ids
                or job.get("run_id") != run["id"] or job.get("head_sha") != revision
                or job.get("status") != "completed" or job.get("conclusion") != "success"):
            raise ValueError("CI jobs are ambiguous, incomplete, skipped, failed or source-mismatched")
        names.add(name)
        ids.add(ident)
    if names != REQUIRED_JOBS:
        raise ValueError("required complete source-verification gates are missing or unknown")
    # The API run URL is not accepted as an authority or followed as a destination.
    return {"source_revision": revision, "repository": REPOSITORY,
            "workflow": WORKFLOW, "run_id": run["id"], "run_attempt": run["run_attempt"],
            "jobs": len(jobs), "status": "verified",
            "url": f"https://github.com/{REPOSITORY}/actions/runs/{run['id']}"}


def api(path, paginate=False):
    command = ["gh", "api", "--hostname", "github.com", path]
    if paginate:
        command += ["--paginate", "--slurp"]
    try:
        result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=45)
        return json.loads(result.stdout)
    except (OSError, subprocess.SubprocessError, ValueError) as error:
        raise ValueError("GitHub CI evidence unavailable; no local or older-run fallback") from error


def main():
    if len(sys.argv) != 2:
        raise ValueError("usage: verify-ci-evidence.py SOURCE_REVISION")
    revision = validate_sha(sys.argv[1])
    workflow = api(f"repos/{REPOSITORY}/actions/workflows/ci.yml")
    if not positive(workflow.get("id")) or workflow.get("path") != WORKFLOW or workflow.get("state") != "active":
        raise ValueError("canonical CI workflow unavailable")
    pages = api(f"repos/{REPOSITORY}/actions/workflows/ci.yml/runs?head_sha={revision}&event=push&per_page=100", True)
    runs = [run for page in pages for run in page["workflow_runs"]]
    selected = select_run(runs, revision)
    # Re-read the exact run/attempt so a retry cannot reuse a former green verdict.
    run = api(f"repos/{REPOSITORY}/actions/runs/{selected['id']}")
    if run.get("run_attempt") != selected.get("run_attempt"):
        raise ValueError("CI attempt changed during evidence readback")
    attempt = run.get("run_attempt")
    if not positive(attempt):
        raise ValueError("CI attempt unavailable")
    # GitHub's latest-job projection includes unchanged successful jobs during
    # failed-jobs-only reruns. The whole run and attempt must remain green and
    # stable before/after; an old green run/attempt is never a fallback.
    pages = api(f"repos/{REPOSITORY}/actions/runs/{run['id']}/jobs?filter=latest&per_page=100", True)
    jobs = [job for page in pages for job in page["jobs"]]
    receipt = validate(run, jobs, revision, workflow["id"])
    final = api(f"repos/{REPOSITORY}/actions/runs/{run['id']}")
    if any(final.get(key) != run.get(key) for key in ("run_attempt", "head_sha", "status", "conclusion", "updated_at")):
        raise ValueError("CI state changed during evidence readback")
    print(json.dumps(receipt, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError) as error:
        print("release CI evidence denied: " + str(error), file=sys.stderr)
        sys.exit(1)
