#!/bin/sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
proof_root=$(mktemp -d "$repository_root/.aegis-authority-denial-XXXXXXXX")
fuzz_time=${AEGIS_AUTHORITY_FUZZTIME:-5s}

cleanup() { rm -rf -- "$proof_root"; }
trap cleanup EXIT HUP INT TERM
mkdir -m 0700 -- "$proof_root/tmp"

export TMPDIR="$proof_root/tmp"

cd -- "$repository_root"

printf '%s\n' 'authority denial matrix: crash and corruption'
go test ./internal/persistence/authority/badger \
  -count=1 \
  -run 'Test(RestartAfterProcessCrashFailsClosedBeforeAuthorityRead|OpenRejectsCorruptActiveMarker|AuthorityRepositoryRejectsMalformedTruncatedAndSubstitutedState|AuthorityAuditDeliveryAndReadinessFailClosedOnSubstitution|AuthorityReadsFailClosedOnDerivedRecordSubstitution|RebuildAuthorityProjectionsIsAtomicOnCorruptCanonicalState)$'

printf '%s\n' 'authority denial matrix: race and three generated canaries'
go test -race ./internal/persistence/authority/badger \
  -count=1 \
  -run 'Test(ConcurrentAuthorityCreationNeverCreatesAmbiguousSession|ProcessAuthorityCommandConcurrentExactRetriesDeduplicate|AuthorityRepositoryThreeCanaryDenialsLeaveNoSecretBytes)$'

printf '%s\n' 'authority denial matrix: bounded key-codec fuzz campaign'
go test ./internal/persistence/authority/badger \
  -run '^$' \
  -fuzz '^FuzzBinaryKeyDecodeCanonicalRoundTrip$' \
  "-fuzztime=$fuzz_time"

printf '%s\n' 'authority denial matrix: PASS'
