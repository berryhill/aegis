SHELL := /bin/sh

VERSION ?= 0.1.0
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.6.0

.PHONY: verify release-review release

verify:
	go mod tidy
	git diff --exit-code -- go.mod go.sum
	sh scripts/release_test.sh
	test -z "$$(gofmt -l ./cmd ./internal)"
	go build ./cmd/aegis
	go test ./...
	go test -race ./...
	go vet ./...
	$(GOVULNCHECK) ./...

release-review:
	@context="$$(git status --short; git diff -- CHANGELOG.md; git log -1 --oneline; git show HEAD:Makefile; git show HEAD:scripts/release.sh; git show HEAD:.github/workflows/release.yml)"; \
	hermes -t todo -z "Advisory release review only. Do not authorize the release and do not call tools. Review this Aegis v$(VERSION) release context for obvious versioning, changelog, or repository-state problems. Return a concise review; the authenticated operator and deterministic Make target retain all authority. Context follows: $$context"

release:
	./scripts/release.sh "$(VERSION)"
