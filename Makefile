# drover - developer workflow targets.
#
#   make            build + vet + lint + tests (unit and race): the local bar
#   make build      build all packages and the drover binary
#   make test       go test ./...
#   make race       go test -race ./...  (includes the crash-resume harness)
#   make vet        go vet ./...
#   make lint       gofmt cleanliness + staticcheck
#   make cover      coverage summary
#   make mutate     mutation gate with gremlins (the seam, the wiring, the CLI)
#   make tidy       go mod tidy
#   make fmt        gofmt the tree
#   make clean      remove build artifacts

GO ?= go
BINARY := drover
PKG := ./...

# Pinned so CI and local runs agree; these are run with `go run` / `go install`,
# not module dependencies. Bump alongside the sibling repos (rerun, leash).
STATICCHECK_VERSION ?= 2025.1.1
# Matches .github/workflows/mutation.yml.
GREMLINS_VERSION ?= v0.6.0

.PHONY: all build vet lint test race cover mutate tidy fmt clean

all: build vet lint test race

build:
	$(GO) build $(PKG)
	$(GO) build -o $(BINARY) ./cmd/drover

vet:
	$(GO) vet $(PKG)

# Formatting and static-analysis gate: gofmt must be clean and staticcheck must
# pass. drover's docs use Unicode (em dashes, mermaid), so there is deliberately
# no ASCII-only gate, matching rerun.
lint:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi; \
	echo "gofmt: ok"
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

test:
	$(GO) test $(PKG)

race:
	$(GO) test -race $(PKG)

cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -n 1

# Mutation testing with gremlins (https://github.com/go-gremlins/gremlins),
# mirroring .github/workflows/mutation.yml: mutate the governor seam (provider),
# the engine wiring (runner), and the CLI's provider routing (cmd/drover).
# gremlins unleash takes one path, so loop. GOFLAGS=-count=1 stops the test cache
# from handing later packages a cached, coverage-less baseline. The gate is
# advisory: it reports, it does not block.
mutate:
	@command -v gremlins >/dev/null 2>&1 || { \
		echo "gremlins not installed."; \
		echo "install: go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)"; \
		exit 1; }
	@export GOFLAGS=-count=1; \
	for pkg in ./provider ./runner ./cmd/drover; do \
		echo "== gremlins unleash $$pkg =="; \
		gremlins unleash "$$pkg" || exit 1; \
	done

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt $(PKG)

clean:
	rm -f $(BINARY) coverage.out
	$(GO) clean
