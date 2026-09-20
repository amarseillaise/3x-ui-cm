SHELL := /bin/bash
COMPOSE := docker compose --project-directory . -f deploy/docker-compose.yml
# Portable Node (make tools-node) takes precedence over a system install.
NODE_VERSION ?= v22
NODE_BIN := $(if $(wildcard .tools/node/bin/node),$(CURDIR)/.tools/node/bin:,)

.PHONY: build vet test check web web-typecheck tools-node run migrate compose-up compose-edge-up compose-down compose-logs backup gen-secrets sub-template

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

check: vet test build web-typecheck

## Build the frontend into internal/webdist/dist (Node 22: system or .tools/node).
web:
	cd web && PATH=$(NODE_BIN)$$PATH npm ci --no-audit --no-fund && PATH=$(NODE_BIN)$$PATH npm run build

web-typecheck:
	cd web && PATH=$(NODE_BIN)$$PATH npm run typecheck

## Download a portable Node 22 for this OS/arch into .tools/node (no root, gitignored).
NODE_OS := $(shell uname -s | tr A-Z a-z)
NODE_ARCH := $(shell uname -m | sed -e 's/x86_64/x64/' -e 's/aarch64/arm64/')
tools-node:
	rm -rf .tools/node && mkdir -p .tools/node && name=$$(curl -sL https://nodejs.org/dist/latest-$(NODE_VERSION).x/SHASUMS256.txt | grep -o 'node-$(NODE_VERSION)[0-9.]*-$(NODE_OS)-$(NODE_ARCH)\.tar\.gz' | head -1) \
	  && curl -sL -o .tools/node.tar.gz "https://nodejs.org/dist/latest-$(NODE_VERSION).x/$$name" \
	  && tar -xzf .tools/node.tar.gz -C .tools/node --strip-components=1 && rm .tools/node.tar.gz \
	  && .tools/node/bin/node --version

run:
	go run ./cmd/server serve

migrate:
	go run ./cmd/server migrate

compose-up:
	$(COMPOSE) up -d --build

compose-edge-up:
	$(COMPOSE) --profile edge up -d --build

compose-down:
	$(COMPOSE) down

compose-logs:
	$(COMPOSE) logs -f --tail=200 app

## Consistent SQLite copy from the running container into ./backups.
backup:
	mkdir -p backups
	$(COMPOSE) exec -T app sqlite3 /data/app.db ".backup /data/backup.tmp"
	$(COMPOSE) cp app:/data/backup.tmp backups/app-$$(date +%Y%m%d-%H%M%S).db
	$(COMPOSE) exec -T app rm -f /data/backup.tmp
	ls -1 backups | tail -1

## Render the 3x-ui subscription-page template that sends browsers to the cabinet (needs APP_BASE_URL).
sub-template:
	mkdir -p sub_template && go run ./cmd/server sub-template > sub_template/index.html \
	  && echo "sub_template/index.html: copy to the panel host (e.g. /etc/3x-ui/sub_templates/cabinet/) and set Sub Theme Directory"

## Print fresh secrets for .env.
gen-secrets:
	@echo "ADMIN_TOKEN=$$(openssl rand -hex 32)"
	@echo "ADMIN_LINK_SECRET=$$(openssl rand -hex 32)"
	@echo "SESSION_SECRET=$$(openssl rand -hex 32)"
