#!/usr/bin/env python3
"""Assert the fleet orchestration source retains its fail-closed authority contract."""

from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
SERVICE = ROOT / "internal" / "orchestration" / "fleet_service.go"
TEST = ROOT / "internal" / "orchestration" / "fleet_service_test.go"


def require(text: str, pattern: str, message: str) -> None:
    if re.search(pattern, text, re.MULTILINE | re.DOTALL) is None:
        raise AssertionError(message)


def main() -> int:
    service = SERVICE.read_text(encoding="utf-8")
    tests = TEST.read_text(encoding="utf-8")

    actions = {
        "FleetActionRegister",
        "FleetActionAgentRevision",
        "FleetActionLoopValidate",
        "FleetActionLoopPublish",
        "FleetActionGraphValidate",
        "FleetActionGraphPublish",
        "FleetActionSubmission",
        "FleetActionQueueAdmission",
        "FleetActionClaim",
        "FleetActionReclaim",
        "FleetActionRuntimeEffect",
        "FleetActionEvidenceVerify",
        "FleetActionDisposition",
    }
    missing = sorted(action for action in actions if action not in service)
    if missing:
        raise AssertionError(f"missing contextual-readiness actions: {', '.join(missing)}")

    for state in ("ready", "denied", "unavailable", "degraded_repair_required", "empty"):
        require(service, rf'"{re.escape(state)}"', f"missing readiness state {state}")

    require(
        service,
        r"GetAuthorityContext\(ctx, ref\.ID\).*?authority\.Digest != ref\.Digest.*?GetMandate.*?ValidateAuthorityContext.*?AuthorityAdmission",
        "authority resolution must bind exact context digest, mandate, and fresh admission",
    )
    require(
        service,
        r"subject\.ID != mandate\.Subject\.ID.*?subject\.PrincipalID != mandate\.Subject\.PrincipalID",
        "authenticated subject must exactly match the mandate subject",
    )
    require(
        service,
        r"Submission\{.*?Authority: authorityRef.*?MandateID: mandate\.ID.*?Runtime: runtimeID",
        "submission must retain exact authority, mandate, and runtime",
    )
    require(
        service,
        r"Item\{.*?Authority: authorityRef",
        "queue item must retain the exact authority reference",
    )
    require(
        service,
        r"GraphRun\{.*?Authority: authorityRef",
        "GraphRun must retain the exact authority reference",
    )
    require(
        service,
        r"if request\.State == execution\.StateSucceeded.*?FleetActionDisposition",
        "successful disposition must repeat fresh contextual readiness",
    )

    constructor = service[service.index("func NewFleetService"):service.index("func (service *FleetService) Readiness")]
    if "credential" in constructor.lower():
        raise AssertionError("optional credentials are coupled to fleet service construction")

    require(
        tests,
        r"accepted\.Submission\.Authority != authorityRef.*?accepted\.QueueItem\.Authority != authorityRef.*?accepted\.GraphRun\.Authority != authorityRef",
        "tests must read back authority across submission, queue item, and GraphRun",
    )
    require(tests, r"wrong\.Authority\.Digest", "tests must exercise authority digest substitution")

    print("authority binding readback: ok")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, OSError, ValueError) as error:
        print(f"authority binding readback: FAIL: {error}", file=sys.stderr)
        raise SystemExit(1)
