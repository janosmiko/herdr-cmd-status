.DEFAULT_GOAL := help

.PHONY: help test lint check hooks restart

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

test: ## Run the unit tests
	node --test

lint: ## Check the shell snippets and the watcher syntax
	shellcheck -s bash shell/cmd-status.bash
	zsh -n shell/cmd-status.zsh
	node --check bin/watch.js

check: lint test ## Run lint and tests

hooks: ## Use the repo git hooks (runs make check before each commit)
	git config core.hooksPath .githooks

restart: ## Restart the watcher of a linked checkout
	herdr plugin action invoke janosmiko.herdr-cmd-status.restart
