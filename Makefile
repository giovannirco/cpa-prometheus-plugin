PLUGIN_ID ?= cpa-prometheus
VERSION ?= 0.1.8
DIST ?= dist
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
EXT := so
ifeq ($(GOOS),darwin)
EXT := dylib
endif
ifeq ($(GOOS),windows)
EXT := dll
endif

.PHONY: test build package fmt tidy

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

tidy:
	go mod tidy

test:
	go test ./...

build:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -buildmode=c-shared -trimpath -ldflags='-s -w -X github.com/giovannirco/cpa-prometheus-plugin/internal/plugin.PluginVersion=$(VERSION)' -o $(DIST)/$(PLUGIN_ID).$(EXT) ./cmd/plugin
	rm -f $(DIST)/$(PLUGIN_ID).h

package: build
	bash scripts/package-release.sh $(VERSION) $(DIST)
