.PHONY: build frontend

VERSION ?= 0.1.0

build: frontend
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o tanoimg ./cmd/tanoimg

frontend: frontend/node_modules/.package-lock.json
	npm run build --prefix frontend

frontend/node_modules/.package-lock.json: frontend/package.json frontend/package-lock.json
	npm ci --prefix frontend
