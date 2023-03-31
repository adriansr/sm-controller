##@ Linting

SH_FILES ?= $(shell $(ROOTDIR)/scripts/list-sh-scripts)

ifeq ($(origin GOLANGCI_LINT),undefined)
GOLANGCI_LINT ?= $(ROOTDIR)/scripts/go/bin/golangci-lint
LOCAL_GOLANGCI_LINT = yes
endif

ifeq ($(LOCAL_GOLANGCI_LINT),yes)
$(GOLANGCI_LINT): scripts/go/go.mod scripts/go/go.sum
	$(S) cd scripts/go && \
		$(GO) mod download && \
		$(GO) build -o $(GOLANGCI_LINT) github.com/golangci/golangci-lint/cmd/golangci-lint
endif

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
	$(S) echo "lint via golangci-lint"
	$(S) $(GOLANGCI_LINT) run \
		$(GOLANGCI_LINT_MOD_FLAGS) \
		--config ./scripts/go/configs/golangci.yml \
		$(GO_PKGS)

.PHONY: go-vet
go-vet:
	$(S) echo "lint via go vet"
	$(S) $(GO) vet $(GO_BUILD_MOD_FLAGS) $(GO_PKGS)

.PHONY: lint-go
lint-go: go-vet golangci-lint ## Run all Go code checks.

.PHONY: lint
lint: lint-go lint-sh ## Run all code checks.

.PHONY: lint-sh
lint-sh: ## Run all shell code checks.
	$(S) echo "lint via shellcheck"
	$(S) docker run --rm -v '$(ROOTDIR):/mnt' koalaman/shellcheck:stable -a --color=auto --shell=sh $(SH_FILES)
