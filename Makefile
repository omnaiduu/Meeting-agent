.PHONY: dev dev-server dev-web build build-web build-server test vet lint sqlc docker run-xvfb joinmeet

BIN := bin/meeting-agent

## dev: run Go server and Vite dev server together (Ctrl+C stops both)
dev:
	@$(MAKE) -j2 dev-server dev-web

dev-server:
	DEV_CORS=true go run ./cmd/server

dev-web:
	cd web && npm run dev

## build: build web, then a single Go binary with the web app embedded
build: build-web build-server

build-web:
	cd web && npm ci && npm run build

build-server:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/server

## run-xvfb: run the server on Linux without a desktop (Chrome needs a display)
run-xvfb:
	xvfb-run -a --server-args="-screen 0 1280x720x24" go run ./cmd/server

## joinmeet: manually test the bot, e.g. make joinmeet URL=https://meet.google.com/abc-defg-hij
joinmeet:
	go run ./cmd/joinmeet -url "$(URL)"

test:
	go test ./...

vet:
	go vet ./...

lint: vet
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

## sqlc: regenerate typed DB code from internal/store/queries
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

docker:
	docker build -f deploy/Dockerfile -t meeting-agent .
