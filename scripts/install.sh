#!/bin/bash

# Install script for Karoo - automatically detects install location
# Usage: scripts/install.sh <binary_path> <config_source>

set -e

BINARY_PATH="$1"
CONFIG_SOURCE="$2"
BINARY_NAME="karoo"

if [ -z "$BINARY_PATH" ] || [ -z "$CONFIG_SOURCE" ]; then
    echo "Usage: $0 <binary_path> <config_source>"
    exit 1
fi

# Detect install location based on user privileges
if [ "$(id -u)" -eq 0 ]; then
    # Root user - install system-wide
    BIN_DIR="/usr/local/bin"
    CONFIG_DIR="/etc/karoo"
    echo "Installing system-wide (root detected)"
else
    # Regular user - install to user directory
    BIN_DIR="$HOME/.local/bin"
    CONFIG_DIR="$HOME/.config/karoo"
    echo "Installing to user directory (non-root detected)"
fi

# Install binary
echo "Installing binary to $BIN_DIR..."
mkdir -p "$BIN_DIR"
install -m 755 "$BINARY_PATH" "$BIN_DIR/$BINARY_NAME"

# Install config
echo "Installing configuration to $CONFIG_DIR..."
mkdir -p "$CONFIG_DIR"
CONFIG_DEST="$CONFIG_DIR/config.json"

if [ -f "$CONFIG_DEST" ]; then
    install -m 644 "$CONFIG_SOURCE" "$CONFIG_DEST.example"
    echo "Config preserved; example at $CONFIG_DEST.example"
else
    install -m 644 "$CONFIG_SOURCE" "$CONFIG_DEST"
fi

echo "Installation complete!"
echo "Binary: $BIN_DIR/$BINARY_NAME"
echo "Config: $CONFIG_DEST"