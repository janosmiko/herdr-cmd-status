.DEFAULT_GOAL := help

.PHONY: help build test lint check hooks restart

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

build: ## Build bin/herdr-cmd-status
	CGO_ENABLED=0 go build -trimpath -o bin/herdr-cmd-status .

test: ## Run the tests
	go test -race ./...

lint: ## Check the Go code, the shell snippets and the install script
	test -z "$$(gofumpt -l .)"
	go vet ./...
	shellcheck -s bash shell/cmd-status.bash
	shellcheck -s sh scripts/install.sh
	zsh -n shell/cmd-status.zsh

check: lint test ## Run lint and tests

hooks: ## Use the repo git hooks (runs make check before each commit)
	git config core.hooksPath .githooks

restart: build ## Build, then restart the watcher of a linked checkout
	herdr plugin action invoke janosmiko.herdr-cmd-status.restart
