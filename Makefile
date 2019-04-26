all: generate build

PACKAGE=github.com/danehans/external-dns-operator
MAIN_PACKAGE=$(PACKAGE)/cmd/externaldns-operator

BIN=$(lastword $(subst /, ,$(MAIN_PACKAGE)))

GO_BUILD_RECIPE=CGO_ENABLED=0 go build -o $(BIN) $(MAIN_PACKAGE)

TEST ?= .*

.PHONY: build
build:
	$(GO_BUILD_RECIPE)

.PHONY: generate
generate: crd
	hack/update-generated-bindata.sh

# Generate IngressController CRD from vendored API spec.
.PHONY: crd
crd:
	go run ./vendor/github.com/openshift/library-go/cmd/crd-schema-gen/main.go --apis-dir vendor/github.com/danehans/api

# Do not write the IngressController CRD, only compare and return (code 1 if dirty).
.PHONY: verify-crd
verify-crd:
	go run ./vendor/github.com/openshift/library-go/cmd/crd-schema-gen/main.go --apis-dir vendor/github.com/danehans/api --verify-only

.PHONY: test
test: verify
	go test ./...

.PHONY: release-local
release-local:
	MANIFESTS=$(shell mktemp -d) hack/release-local.sh

.PHONY: test-e2e
test-e2e:
	KUBERNETES_CONFIG="$(KUBECONFIG)" WATCH_NAMESPACE=openshift-externaldns-operator go test -count 1 -v -tags e2e -run "$(TEST)" ./...

.PHONY: clean
clean:
	go clean
	rm -f $(BIN)

.PHONY: verify
verify: verify-crd
	hack/verify-gofmt.sh
	hack/verify-generated-bindata.sh

.PHONY: uninstall
uninstall:
	hack/uninstall.sh
