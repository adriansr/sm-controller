##@ Helpers

ifeq ($(origin GITIGNORE_GEN),undefined)
GITIGNORE_GEN ?= $(ROOTDIR)/scripts/go/bin/gitignore-gen
LOCAL_GITIGNORE_GEN = yes
endif

ifeq ($(origin BRA_BIN),undefined)
BRA_BIN ?= $(ROOTDIR)/scripts/go/bin/bra
LOCAL_BRA = yes
endif

ifeq ($(LOCAL_BRA),yes)
$(BRA_BIN): scripts/go/go.mod
	$(S) cd scripts/go && \
		$(GO) mod download && \
		$(GO) build -o "$@" github.com/unknwon/bra
endif

.PHONY: run
run: $(BRA_Bin) ## Build and run web server on filesystem changes.
	$(S) $(GO_BUILD_MOD_FLAGS) $(BRA_BIN) run

.PHONY: clean
clean: ## Clean up intermediate build artifacts.
	$(S) echo "Cleaning intermediate build artifacts..."
	$(V) rm -rf node_modules
	$(V) rm -rf public/build
	$(V) rm -rf dist/build
	$(V) rm -rf dist/publish

.PHONY: distclean
distclean: clean ## Clean up all build artifacts.
	$(S) echo "Cleaning all build artifacts..."
	$(V) git clean -Xf

.PHONY: update-tools
update-tools: ## Update tools
	$(S) echo "Updating tools..."
	$(V) cd scripts/go && ./update
	$(S) echo "Done."

ifeq ($(LOCAL_GITIGNORE_GEN),yes)
$(GITIGNORE_GEN): scripts/go/go.mod
	$(S) cd scripts/go && \
		$(GO) mod download && \
		$(GO) build -o "$@" github.com/mem/gitignore-gen
endif

.PHONY: update-gitignore
update-gitignore: $(GITIGNORE_GEN)
	$(V) $(GITIGNORE_GEN) -config-filename scripts/go/configs/gitignore.yaml > $(ROOTDIR)/.gitignore

.PHONY: docker-build
docker-build:
	$(ROOTDIR)/scripts/docker_build build

.PHONY: docker-image
docker-image: docker-build
	$(S) docker build -t $(DOCKER_TAG) ./

.PHONY: docker-push
docker-push:  docker
	$(S) docker push $(DOCKER_TAG)
	$(S) docker tag $(DOCKER_TAG) $(DOCKER_TAG):$(BUILD_VERSION)
	$(S) docker push $(DOCKER_TAG):$(BUILD_VERSION)

.PHONY: testdata
testdata: ## Update golden files for tests.
	$(S) true

.PHONY: drone
drone: .drone.yml ## Update drone files
	$(S) true

DRONE_SOURCE_FILES := $(wildcard scripts/go/configs/drone/*.jsonnet scripts/go/configs/drone/*.libsonnet)

.drone.yml: $(DRONE_SOURCE_FILES)
	$(S) echo 'Regenerating $@...'
	$(V) drone jsonnet --source "$<" --target "$@" --stream --format --extVar "go_version=$(GO_VERSION)"
	$(V) drone lint "$@"
	$(V) drone sign --save "$(GH_REPO_NAME)" "$@"

.PHONY: dronefmt
dronefmt: ## Format drone jsonnet files
	$(S) $(foreach src, $(DRONE_SOURCE_FILES), \
		echo "==== Formatting $(src)" ; \
		jsonnetfmt -i --pretty-field-names --pad-arrays --pad-objects --no-use-implicit-plus "$(src)" ; \
	)

.PHONY: update-go-version
update-go-version: ## Update Go version (specify in go.mod)
	$(S) echo 'Updating Go version to $(GO_VERSION)...'
	$(S) cd scripts/go && $(GO) mod edit -go=$(GO_VERSION)
	$(S) sed -i -e 's,^GO_VERSION=.*,GO_VERSION=$(GO_VERSION),' scripts/docker_build
	$(S) $(MAKE) --always-make --no-print-directory .drone.yml S=$(S) V=$(V)

.PHONY: help
help: ## Display this help.
	$(S) awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

