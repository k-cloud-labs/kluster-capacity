# Go information
GO ?= go
GOFMT ?= gofmt "-s"
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
SOURCES := $(shell find . -type f  -name '*.go')

GOFILES := $(shell find . -name "*.go" | grep -v vendor)
TESTFOLDER := $(shell $(GO) list ./... | grep -v examples)
TESTTAGS ?= ""
VETPACKAGES ?= $(shell $(GO) list ./... | grep -v /examples/)

# Git information
GIT_VERSION ?= $(shell git describe --tags --dirty --always)
GIT_COMMIT_HASH ?= $(shell git rev-parse HEAD)
GIT_TREESTATE = "clean"
GIT_DIFF = $(shell git diff --quiet >/dev/null 2>&1; if [ $$? -eq 1 ]; then echo "1"; fi)
ifeq ($(GIT_DIFF), 1)
    GIT_TREESTATE = "dirty"
endif
BUILDDATE = $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := "-X github.com/k-cloud-labs/kluster-capacity/pkg/version.gitVersion=$(GIT_VERSION) \
                      -X github.com/k-cloud-labs/kluster-capacity/pkg/version.gitCommit=$(GIT_COMMIT_HASH) \
                      -X github.com/k-cloud-labs/kluster-capacity/pkg/version.gitTreeState=$(GIT_TREESTATE) \
                      -X github.com/k-cloud-labs/kluster-capacity/pkg/version.buildDate=$(BUILDDATE)"

# Set your version by env or using latest tags from git
VERSION?=""
ifeq ($(VERSION), "")
    LATEST_TAG=$(shell git describe --tags --always)
    ifeq ($(LATEST_TAG),)
        # Forked repo may not sync tags from upstream, so give it a default tag to make CI happy.
        VERSION="unknown"
    else
        VERSION=$(LATEST_TAG)
    endif
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: checkall
checkall: fmt-check vet ## Do all check
	hack/verify-staticcheck.sh
	hack/verify-import-aliases.sh

.PHONY: build
build: $(SOURCES) ## Build kluster-capacity webhook binary file
	@CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o kluster-capacity \
		main.go

.PHONY: clean
clean: ## Clean kluster-capacity webhook binary file
	@rm -rf kluster-capacity

.PHONY: fmt
fmt: ## Format project files
	@$(GOFMT) -w $(GOFILES)

.PHONY: fmt-check
fmt-check: ## Check project files format info
	@diff=$$($(GOFMT) -d $(GOFILES)); \
	if [ -n "$$diff" ]; then \
		echo "Please run 'make fmt' and commit the result:"; \
		echo "$${diff}"; \
		exit 1; \
	fi;

.PHONY: vet
vet:
	@$(GO) vet $(VETPACKAGES)

.PHONY: test
test: fmt-check vet ## Run project unit test and generate coverage result
	echo "mode: count" > coverage.out
	for d in $(TESTFOLDER); do \
		$(GO) test -tags $(TESTTAGS) -v -covermode=count -coverprofile=profile.out $$d > tmp.out; \
		cat tmp.out; \
		if grep -q "^--- FAIL" tmp.out; then \
			rm tmp.out; \
			exit 1; \
		elif grep -q "build failed" tmp.out; then \
			rm tmp.out; \
			exit 1; \
		elif grep -q "setup failed" tmp.out; then \
			rm tmp.out; \
			exit 1; \
		fi; \
		if [ -f profile.out ]; then \
			cat profile.out | grep -v "mode:" >> coverage.out; \
			rm profile.out; \
		fi; \
	done
