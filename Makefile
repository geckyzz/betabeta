BIN := betabeta
GO := go
SRC_DIR := src

.PHONY: all build run test clean install uninstall systemd-install systemd-uninstall help

all: build

## Build the betabeta binary.
build:
	$(GO) build -o $(BIN) ./$(SRC_DIR)/

## Run betabeta directly from source.
run:
	$(GO) run ./$(SRC_DIR)/

## Run unit tests.
test:
	$(GO) test ./$(SRC_DIR)/...

## Clean build artifacts.
clean:
	rm -f $(BIN)

## Install betabeta to ~/.local/bin and config to ~/.config/betabeta.
install:
	@mkdir -p $(HOME)/.local/bin
	@mkdir -p $(HOME)/.config/betabeta
	$(GO) build -o $(HOME)/.local/bin/$(BIN) ./$(SRC_DIR)/
	@cp config.example.toml $(HOME)/.config/betabeta/config.toml
	@echo "Installed $(BIN) to $(HOME)/.local/bin/"
	@echo "Config:   $(HOME)/.config/betabeta/config.toml"

## Install betabeta + systemd user service (dynamic paths).
systemd-install: install
	@mkdir -p $(HOME)/.config/systemd/user
	@sed -e "s|__BIN_PATH__|$(HOME)/.local/bin/$(BIN)|" \
	    -e "s|__CONFIG_DIR__|$(HOME)/.config/betabeta|" \
	    -e "s|__CONFIG_TOML__|$(HOME)/.config/betabeta/config.toml|" \
	    misc/betabeta.service > $(HOME)/.config/systemd/user/$(BIN).service
	@echo "Installed systemd service to $(HOME)/.config/systemd/user/$(BIN).service"
	@echo "Run: systemctl --user daemon-reload && systemctl --user enable --now $(BIN)"

## Remove systemd user service.
systemd-uninstall:
	@rm -f $(HOME)/.config/systemd/user/$(BIN).service
	@echo "Removed systemd service $(BIN)"

## Uninstall betabeta.
uninstall: systemd-uninstall
	@rm -f $(HOME)/.local/bin/$(BIN)
	@rm -rf $(HOME)/.config/betabeta
	@echo "Uninstalled $(BIN)"

help:
	@grep -E '^## .*' $(MAKEFILE_LIST) \
	    | awk 'BEGIN {FS = ":.*?## "}; {printf "    %-22s %s\n", $$1, $$2}'
