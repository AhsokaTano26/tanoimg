.PHONY: build

VERSION ?= 0.1.0

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o tanoimg ./cmd/tanoimg
