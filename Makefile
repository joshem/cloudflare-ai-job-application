# Builds the Go Worker to WebAssembly and wraps common Wrangler commands.
#
#   make build            # Go (standard toolchain) -> build/app.wasm
#   make build TINYGO=1   # TinyGo -> smaller binary (needs TinyGo 0.42.0+)
#   make dev              # run locally with Wrangler
#   make deploy           # build and deploy
#   make clean

BUILD_DIR := build
ASSETS_GEN := github.com/syumai/workers-go/cmd/workers-assets-gen

.PHONY: build
build:
	mkdir -p $(BUILD_DIR)
ifeq ($(TINYGO),1)
	go run $(ASSETS_GEN) -mode=tinygo
	tinygo build -o ./$(BUILD_DIR)/app.wasm -target wasm -no-debug .
else
	go run $(ASSETS_GEN) -mode=go
	GOOS=js GOARCH=wasm go build -o ./$(BUILD_DIR)/app.wasm .
endif

.PHONY: dev
dev:
	npx wrangler dev

.PHONY: deploy
deploy:
	npx wrangler deploy

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)