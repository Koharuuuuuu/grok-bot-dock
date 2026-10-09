.PHONY: dev build test run clean

dev:
	cd frontend && npm run dev

build:
	cd frontend && npm ci && npm run build
	go build -o bolt-dock ./cmd/server

test:
	go test ./...
	cd frontend && npm test

run:
	go run ./cmd/server

clean:
	rm -f bolt-dock
