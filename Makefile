# statisfy — local build helpers (release artifacts are built by CI).
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test vet fmt check install release clean

build:
	go build -ldflags "$(LDFLAGS)" -o statisfy ./cmd/statisfy

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/statisfy

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -l .

check: fmt vet test build
	@echo "all checks passed"

release: 
	@set -e; \
	for t in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64; do \
	  os=$${t%-*}; arch=$${t#*-}; ext=""; \
	  [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "building $$t"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statisfy-$$t$$ext ./cmd/statisfy; \
	done; \
	cd dist && sha256sum statisfy-* > checksums.txt; \
	echo "artifacts in dist/"

clean:
	rm -rf dist statisfy statisfy.exe
