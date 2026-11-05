VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all web build run mock test e2e screenshots docs-images docker clean

all: web build

web:
	cd web && (if [ -f package-lock.json ]; then npm ci; else npm install; fi) && npm run build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/genkan ./cmd/genkan

run: build
	GENKAN_DATA=./data ./bin/genkan

test:
	go vet ./...
	go test -race ./...
	bash scripts/next-version_test.sh

e2e:
	cd e2e && (if [ -f package-lock.json ]; then npm ci; else npm install; fi) && npx playwright install chromium && npx playwright test

screenshots:
	cd e2e && (if [ -f package-lock.json ]; then npm ci; else npm install; fi) && npx playwright install chromium && npm run screenshots

# compose the optimized README images (docs/images) from the raw captures
docs-images:
	cd e2e && npm run docs-images

mock:
	cd web && npm run mock

docker:
	docker build --build-arg VERSION=$(VERSION) -t genkan:latest .

clean:
	rm -rf bin web/dist/assets
