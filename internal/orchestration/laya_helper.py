"""Fixed stdin/stdout Laya v1 proposal protocol, embedded by decision_laya.go.

The controller owns all questions. This process has no admission/authority role.
Run with an isolated Python interpreter that has the local `laya` package installed;
never point it at an untrusted Python executable or give it privileged secrets.
"""
import json
import math
import sys

QUESTIONS = {
    "gate": {
        "specified": "Does the user request one concrete actionable outcome, rather than only discuss an idea?",
        "result_defined": "Does the request identify an observable result or artifact that could count as success?",
    },
    "verdict": {
        "done": "Does the report describe a completed result rather than merely an intention or partial work?",
        "stays_in_scope": "Does the reported work stay within the original request, with no unrelated changes?",
        "fulfills": "Does the reported result address the requested outcome?",
        "works": "Does the report include specific observed checks or evidence that the result works?",
        "practices": "Does the report show reasonable task-appropriate practices without demanding perfection?",
    },
}


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def exact_keys(value, expected):
    if type(value) is not dict or set(value) != set(expected):
        raise ValueError("invalid fields")


def decide(request, router_factory):
    exact_keys(request, ("version", "kind", "state"))
    if type(request["version"]) is not int or request["version"] != 1:
        raise ValueError("invalid version")
    kind, state = request["kind"], request["state"]
    if type(kind) is not str or kind not in QUESTIONS:
        raise ValueError("invalid kind")
    if type(state) is not str or not state.strip() or len(state.encode()) > 65536:
        raise ValueError("invalid state")
    schema = {
        name: {
            "type": "choice",
            "criteria": {"yes": "yes, supported by the supplied state", "no": "no or insufficient evidence"},
            "instructions": instruction,
        }
        for name, instruction in QUESTIONS[kind].items()
    }
    prediction = router_factory().predict(state, schema)
    if type(prediction) is not dict or "answers" not in prediction:
        raise ValueError("missing answers")
    answers = prediction["answers"]
    exact_keys(answers, QUESTIONS[kind])
    typed = {}
    for name, answer in answers.items():
        # No coercion of booleans, strings, missing confidence, or ambiguous choices.
        if type(answer) is not dict or not {"choice", "answer_confidence"} <= answer.keys():
            raise ValueError("invalid answer")
        choice, confidence = answer["choice"], answer["answer_confidence"]
        if type(choice) is not str or choice not in ("yes", "no"):
            raise ValueError("ambiguous answer")
        if type(confidence) not in (int, float) or not math.isfinite(confidence) or not 0.6 <= confidence <= 1:
            raise ValueError("low confidence")
        typed[name] = {"choice": choice, "answer_confidence": confidence}
    return {"version": 1, "kind": kind, "answers": typed}


def main():
    try:
        raw = sys.stdin.buffer.read(65537)
        if len(raw) > 65536:
            raise ValueError("input too large")
        request = json.loads(raw, object_pairs_hook=unique_object,
                             parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite")))
        # Import only after shape validation; missing Laya is a denial, not a fallback.
        from laya import Router
        response = decide(request, Router)
        sys.stdout.write(json.dumps(response, allow_nan=False, separators=(",", ":")))
    except Exception:
        # Do not leak prompts, model output, or exception content on stderr.
        sys.exit(1)


if __name__ == "__main__":
    main()
