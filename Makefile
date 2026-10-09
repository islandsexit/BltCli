.DEFAULT_GOAL := build

GO ?= go
GORELEASER ?= goreleaser
ARGS ?=

ifeq ($(OS),Windows_NT)
SHELL := cmd.exe
EXE := .exe
else
EXE :=
endif

BINARY := bin/boltcli$(EXE)

.PHONY: build run test vet check fmt bench coverage race tidy clean release-check snapshot release help

build: | bin
	$(GO) build -trimpath -o $(BINARY) ./cmd

bin:
ifeq ($(OS),Windows_NT)
	if not exist bin mkdir bin
else
	mkdir -p bin
endif

run: build
ifeq ($(OS),Windows_NT)
	.\bin\boltcli.exe $(ARGS)
else
	$(BINARY) $(ARGS)
endif

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: test vet

fmt:
	gofmt -w cmd config domain repository utils cli tui

bench:
	$(GO) test -run=^$$ -bench=. -benchmem ./...

coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

race:
	$(GO) test -race ./...

tidy:
	$(GO) mod tidy

clean:
ifeq ($(OS),Windows_NT)
	if exist bin rmdir /s /q bin
	if exist dist rmdir /s /q dist
	if exist coverage.out del /q coverage.out
	if exist coverage.html del /q coverage.html
else
	rm -rf -- bin dist
	rm -f -- coverage.out coverage.html
endif

release-check:
	$(GORELEASER) check

snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

release:
	$(GORELEASER) release --clean

help:
	@echo build / run ARGS=--help - build or run BoltCLI
	@echo test / vet / check / fmt - development checks
	@echo bench / coverage / race - performance and test reports
	@echo tidy / clean - dependencies and generated artifacts
	@echo release-check / snapshot / release - GoReleaser tasks
	@echo Signature: Written by Dem4ik
