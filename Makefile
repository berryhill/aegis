SHELL := scripts/verify-shell.sh
# Pin Go toolchain to the project's own version. Without this, govulncheck
# triggers GOTOOLCHAIN=auto switching: govulncheck@v1.6.0 itself requires
# only go 1.25+, so it builds with the older toolchain and then fails to
# load packages that require go 1.26 (this project).
export GOTOOLCHAIN := go1.26.6

VERSION ?= 0.2.2
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.6.0

.PHONY: verify console-generate console-verify authority-denial-matrix release-review release

console-generate:
	go generate ./web/console

console-verify:
	python3 scripts/verify-console-vendor.py
	python3 scripts/console_security_test.py
	go generate ./web/console
	git diff --exit-code -- web/console go.mod go.sum

verify:
	$(MAKE) console-verify
	go mod tidy
	git diff --exit-code -- go.mod go.sum
	sh scripts/release_test.sh
	test -z "$$(gofmt -l ./cmd ./internal ./web)"
	go build ./cmd/aegis
	python3 -m unittest scripts/operator_acceptance_poc_test.py
	python3 -m unittest scripts/verify_release_archive_test.py
	./scripts/verify_installed_mvi_test.sh
	./scripts/verify_release_candidate_test.sh
	./scripts/verify-installed-mvi.sh
	go test ./...
	go test -race ./...
	go vet ./...
	$(GOVULNCHECK) ./...

authority-denial-matrix:
	./scripts/verify-authority-denial-matrix.sh

release-review:
	@context="$$(git status --short; git diff -- CHANGELOG.md; git log -1 --oneline; git show HEAD:Makefile; git show HEAD:scripts/release.sh; git show HEAD:.github/workflows/release.yml)"; \
	hermes -t todo -z "Advisory release review only. Do not authorize the release and do not call tools. Review this Aegis v$(VERSION) release context for obvious versioning, changelog, or repository-state problems. Return a concise review; the authenticated operator and deterministic Make target retain all authority. Context follows: $$context"

release:
	./scripts/release.sh "$(VERSION)"
