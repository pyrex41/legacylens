GO := go
GOFMT := gofmt

# Platform detection for library downloads
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
  ifeq ($(UNAME_M),arm64)
    COZO_PLATFORM := aarch64-apple-darwin
    ONNX_PLATFORM := osx-arm64
  else
    COZO_PLATFORM := x86_64-apple-darwin
    ONNX_PLATFORM := osx-x86_64
  endif
else
  COZO_PLATFORM := x86_64-unknown-linux-gnu
  ONNX_PLATFORM := linux-x64
endif

COZO_VERSION := 0.7.6
COZO_LIB_URL := https://github.com/cozodb/cozo/releases/download/v$(COZO_VERSION)/libcozo_c-$(COZO_VERSION)-$(COZO_PLATFORM).a.gz

export CGO_LDFLAGS := -L$(CURDIR)/libs

.PHONY: deps test vet fmt build clean serve docker docker-up docker-down docker-embed-up docker-embed-down cozo-libs validate

cozo-libs:
	@mkdir -p libs
	@if [ ! -f libs/libcozo_c.a ]; then \
		echo "Downloading CozoDB $(COZO_VERSION) for $(COZO_PLATFORM)..."; \
		curl -L -o libs/libcozo_c.a.gz "$(COZO_LIB_URL)"; \
		gunzip -f libs/libcozo_c.a.gz; \
		echo "CozoDB library ready at libs/libcozo_c.a"; \
	else \
		echo "CozoDB library already present"; \
	fi

deps: cozo-libs
	$(GO) mod tidy

test: deps
	$(GO) test ./...

vet: deps
	$(GO) vet ./...

fmt:
	$(GOFMT) -w ./cmd ./internal

build: deps
	$(GO) build -o legacylens ./cmd/legacylens

validate: test vet

serve: build
	./legacylens -repo ./third_party/M_blas -serve

docker:
	docker build -t legacylens .

docker-up:
	docker compose --profile default up --build -d

docker-down:
	docker compose --profile default down

docker-embed-up:
	docker compose --profile embeddings up --build -d

docker-embed-down:
	docker compose --profile embeddings down

clean:
	rm -f legacylens legacylens.db legacylens_cozo.db
