# Shortcuts for the e2e stack (Keycloak + Temporal server + UI) and the Go demo.
# Run `make` with no target to list them.

COMPOSE := docker compose -f e2e/compose.yaml

.DEFAULT_GOAL := help
.PHONY: help up up-broken down stop restart ps logs verify demo test

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-10s %s\n", $$1, $$2}'

up: ## Start the stack with the correct regex and wait until setup is done
	$(COMPOSE) up -d
	@$(MAKE) --no-print-directory _wait

up-broken: ## Restart the server with the broken regex `_ROLE_` (Test D); `make up` puts it back
	PERMISSIONS_REGEX=_ROLE_ $(COMPOSE) up -d
	@$(MAKE) --no-print-directory _wait

down: ## Stop and remove everything, including the namespaces and workflow
	$(COMPOSE) down -v

stop: ## Stop and remove the containers but keep the namespaces and workflow
	$(COMPOSE) down

restart: down up ## Start again from a clean state

ps: ## Show the state of each container
	$(COMPOSE) ps -a

logs: ## Follow the logs; pick services with S="temporal temporal-ui"
	$(COMPOSE) logs -f $(S)

verify: ## Call the server as each test user and print allow/deny (needs curl, jq)
	./e2e/verify.sh

demo: ## Run the Go demo (no Docker needed)
	go run .

test: ## Run go vet and the Go tests, as CI does
	go vet ./...
	go test ./...

# Waits for the one-shot temporal-setup container and fails if it failed.
_wait:
	@echo "Waiting for temporal-setup (about a minute the first time)..."
	@$(COMPOSE) wait temporal-setup >/dev/null || { $(COMPOSE) logs temporal-setup; exit 1; }
	@$(COMPOSE) logs --no-log-prefix temporal-setup | tail -1
	@echo "Ready. UI: http://localhost:8233  (users ns-only, ns-plus-system; password demo-password)"
