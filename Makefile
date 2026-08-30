PLUGIN_ID ?= cpa-prometheus
VERSION ?= 0.1.5
DIST ?= dist

.PHONY: test build package fmt tidy

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

tidy:
	go mod tidy

test:
	go test ./...

build:
	mkdir -p $(DIST)
	CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -ldflags='-s -w -X github.com/giovannirco/cpa-prometheus-plugin/internal/plugin.PluginVersion=$(VERSION)' -o $(DIST)/$(PLUGIN_ID).so ./cmd/plugin
	rm -f $(DIST)/$(PLUGIN_ID).h

package: build
	bash scripts/package-release.sh $(VERSION) $(DIST)
