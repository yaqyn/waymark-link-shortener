.PHONY: build run test vet telemetry-up telemetry-down

build:
	go build -trimpath -ldflags "-X github.com/yaqyn/waymark-link-shortener/internal/build.GitSHA=$$(git rev-parse HEAD) -X github.com/yaqyn/waymark-link-shortener/internal/build.BuildTime=$$(date -u '+%Y-%m-%dT%H:%M:%SZ')" -o waymark .

run:
	go run .

test:
	go test -race ./...

vet:
	go vet ./...

telemetry-up:
	docker compose up -d

telemetry-down:
	docker compose down
