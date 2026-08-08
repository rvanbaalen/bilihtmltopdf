# bilihtmltopdf build targets. `make help` lists them.

BINARY := bin/wkhtmltopdf

.PHONY: help build test e2e fetch-shell release-dry check clean

help: ## List targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

build: ## Build the wkhtmltopdf binary into bin/
	go build -o $(BINARY) ./cmd/wkhtmltopdf

test: ## Run unit tests (no browser needed)
	go test ./internal/... ./cmd/...

e2e: ## Run end-to-end tests (needs Chrome + poppler's pdftotext)
	go test -count=1 ./e2e

fetch-shell: ## Download chrome-headless-shell bundles into third_party/
	./scripts/fetch-headless-shell.sh

release-dry: ## Full goreleaser dry run: snapshot archives + deb/rpm into dist/
	goreleaser release --snapshot --clean

check: ## Validate .goreleaser.yaml
	goreleaser check

clean: ## Remove build and release outputs
	rm -rf bin dist
