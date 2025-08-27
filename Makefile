all: help

TOOLS_DIR := $(shell pwd)/tools/.bin

.PHONY: help
help: Makefile
	@sed -n 's/^##//p' $< | awk 'BEGIN {FS = ":"}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

## test: Run test
.PHONY: test
test:
	@go test -v $(TESTARGS) ./...

## lint: Run lint
.PHONY: lint
lint:
	@./scripts/lint -c .golangci.yml

## e2e-test: Run end-to-end tests
.PHONY: e2e-test
e2e-test:
	@./test/e2e.sh
