# Builds and tests run inside a Docker image so results do not depend on the host toolchain.
GO_IMAGE ?= golang:1.25

# Run as the calling user; caches live in the gitignored .cache/ directory.
DOCKER_RUN = docker run --rm \
	--user $(shell id -u):$(shell id -g) \
	-v $(CURDIR):/src -w /src \
	-e HOME=/tmp -e GOFLAGS=-buildvcs=false \
	-e GOCACHE=/src/.cache/build -e GOMODCACHE=/src/.cache/mod \
	$(GO_IMAGE)

.PHONY: test vet fmt-check bench fuzz tidy

## test: go vet, gofmt check and unit tests with the race detector
test: fmt-check vet
	$(DOCKER_RUN) go test -race ./...

vet:
	$(DOCKER_RUN) go vet ./...

## fmt-check: fail if any file is not gofmt-formatted
fmt-check:
	@out=$$($(DOCKER_RUN) sh -c "find . -name '*.go' -not -path './.cache/*' | xargs gofmt -l"); \
	if [ -n "$$out" ]; then echo "not gofmt-formatted:"; echo "$$out"; exit 1; fi

## bench: run benchmarks (no unit tests)
bench:
	$(DOCKER_RUN) go test -run '^$$' -bench . -benchmem ./...

## fuzz: run every fuzz test for FUZZTIME (default 10s each)
FUZZTIME ?= 10s
fuzz:
	@for f in $$($(DOCKER_RUN) go test -list '^Fuzz' ./ownership | grep '^Fuzz'); do \
		echo "== $$f"; $(DOCKER_RUN) go test ./ownership -run '^$$' -fuzz "^$$f$$" -fuzztime $(FUZZTIME) || exit 1; \
	done

## tidy: go mod tidy (needs network)
tidy:
	$(DOCKER_RUN) go mod tidy
