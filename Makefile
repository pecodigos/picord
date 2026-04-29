.PHONY: build run clean install fmt lint

APP_NAME := picord
BINARY := bin/$(APP_NAME)
SRC := ./cmd/picord

GO := go
GOFLAGS := -ldflags="-s -w"

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) $(SRC)

run: build
	./$(BINARY)

clean:
	rm -rf bin/

install:
	$(GO) install $(GOFLAGS) $(SRC)

fmt:
	$(GO) fmt ./...

lint:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

deps:
	$(GO) get ./...

# Packaging targets
.PHONY: deb appimage

deb: build
	@echo "Creating .deb package..."
	mkdir -p dist/deb/usr/bin
	mkdir -p dist/deb/DEBIAN
	cp $(BINARY) dist/deb/usr/bin/$(APP_NAME)
	@echo "Package: picord" > dist/deb/DEBIAN/control
	@echo "Version: 0.1.0" >> dist/deb/DEBIAN/control
	@echo "Architecture: amd64" >> dist/deb/DEBIAN/control
	@echo "Maintainer: Picord Contributors" >> dist/deb/DEBIAN/control
	@echo "Description: Universal Discord Rich Presence manager for Linux" >> dist/deb/DEBIAN/control
	dpkg-deb --build dist/deb dist/picord_0.1.0_amd64.deb
	rm -rf dist/deb

appimage: build
	@echo "Creating AppImage..."
	mkdir -p dist/AppImage/usr/bin
	cp $(BINARY) dist/AppImage/usr/bin/$(APP_NAME)
	@echo "[Desktop Entry]" > dist/AppImage/picord.desktop
	@echo "Name=Picord" >> dist/AppImage/picord.desktop
	@echo "Exec=$(APP_NAME)" >> dist/AppImage/picord.desktop
	@echo "Type=Application" >> dist/AppImage/picord.desktop
	@echo "Categories=Utility;" >> dist/AppImage/picord.desktop
	@echo "Icon=$(APP_NAME)" >> dist/AppImage/picord.desktop
	@echo "Terminal=false" >> dist/AppImage/picord.desktop
	@echo "AppImage creation requires appimagetool. Skipping for now."
