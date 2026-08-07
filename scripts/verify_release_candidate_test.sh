#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root=$(mktemp -d "$repo/.aegis-release-candidate-test-XXXXXXXX")
cleanup() { rm -rf "$root"; }
trap cleanup EXIT HUP INT TERM

fail_test() {
  printf 'release candidate test failed: %s\n' "$*" >&2
  exit 1
}
expect_failure() {
  expected=$1
  shift
  set +e
  "$@" >"$root/stdout" 2>"$root/stderr"
  status=$?
  set -e
  [ "$status" -ne 0 ] || fail_test "command unexpectedly succeeded: $expected"
  grep -Fq "$expected" "$root/stderr" || {
    printf '%s\n' '--- stderr ---' >&2
    sed -n '1,120p' "$root/stderr" >&2
    fail_test "failure omitted: $expected"
  }
}

cat >"$root/hermes" <<'EOF'
#!/bin/sh
printf 'hermes 0.18.0\n'
EOF
chmod 0700 "$root/hermes"
revision=$(git -C "$repo" rev-parse HEAD)

write_decision() {
  destination=$1
  candidate_version=${2:-1.2.3}
  candidate_revision=${3:-$revision}
  decision=${4:-hold}
  cat >"$destination" <<EOF
{"schema_version":1,"candidate_version":"$candidate_version","source_revision":"$candidate_revision","decision":"$decision","decided_by":"release fixture","rationale":"named acceptance denial test"}
EOF
}
write_decision "$root/decision.json"

expect_failure 'usage: verify-release-candidate.sh' \
  "$repo/scripts/verify-release-candidate.sh"
expect_failure 'required file is missing, not regular, or a symlink:' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/missing-hermes" "$root/work-missing" "$root/decision.json"

ln -s "$root/hermes" "$root/hermes-link"
expect_failure 'required file is missing, not regular, or a symlink:' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/hermes-link" "$root/work-link-hermes" "$root/decision.json"

mkdir "$root/real-work"
ln -s "$root/real-work" "$root/linked-work"
expect_failure 'workspace must not be a symlink:' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/hermes" "$root/linked-work" "$root/decision.json"

write_decision "$root/wrong-version.json" 1.2.4
expect_failure 'decision manifest candidate identity mismatch' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/hermes" "$root/work-wrong-version" "$root/wrong-version.json"

write_decision "$root/wrong-decision.json" 1.2.3 "$revision" approve
expect_failure 'decision must be release, hold, or withdraw' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/hermes" "$root/work-wrong-decision" "$root/wrong-decision.json"

printf '{"schema_version":1,"extra":true}\n' >"$root/extra-field.json"
expect_failure 'decision manifest fields are not exact' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/hermes" "$root/work-extra-field" "$root/extra-field.json"

python3 -c 'import sys; open(sys.argv[1], "wb").write(b" " * (16 * 1024 + 1))' "$root/oversized.json"
expect_failure 'decision manifest exceeds 16 KiB' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$revision" "$root/hermes" "$root/work-oversized" "$root/oversized.json"

wrong_revision=0000000000000000000000000000000000000000
expect_failure 'source revision mismatch:' \
  "$repo/scripts/verify-release-candidate.sh" 1.2.3 "$wrong_revision" "$root/hermes" "$root/work-wrong-revision" "$root/decision.json"

printf 'release candidate named acceptance denial tests passed\n'
