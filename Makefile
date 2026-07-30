VERSION := $(shell tr -d '\r\n' < VERSION)
LDFLAGS := -X hearth/internal/buildinfo.Version=$(VERSION)
WINDOWS_ZIP := dist/hearth-windows-amd64-v$(VERSION).zip

.PHONY: dev-web dev-api build windows-package test

dev-web:
	pnpm --dir web dev

dev-api:
	HEARTH_DEMO=true HEARTH_ADMIN_PASSWORD=admin go run ./cmd/hearth

build:
	pnpm --dir web build
	go build -ldflags="$(LDFLAGS)" -o bin/hearth ./cmd/hearth

windows-package:
	pnpm --dir web build
	mkdir -p dist/windows-amd64
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o dist/windows-amd64/hearth.exe ./cmd/hearth
	cp scripts/install-windows.ps1 scripts/uninstall-windows.ps1 THIRD_PARTY_NOTICES.md VERSION dist/windows-amd64/
	cd dist/windows-amd64 && zip -FS -j -q ../hearth-windows-amd64-v$(VERSION).zip hearth.exe install-windows.ps1 uninstall-windows.ps1 THIRD_PARTY_NOTICES.md VERSION
	cp $(WINDOWS_ZIP) dist/hearth-windows-amd64.zip

test:
	go test ./...
	pnpm --dir web check
