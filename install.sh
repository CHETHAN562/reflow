#!/bin/sh
# install.sh - Installs the latest release of Reflow (t)
# Usage: curl -sSL https://raw.githubusercontent.com/RevereInc/reflow/main/install.sh | sudo bash

set -e

# --- Configuration ---
REPO="RevereInc/reflow"
BINARY_NAME="reflow"
INSTALL_DIR="/usr/local/bin"
# ---

# --- Helper Functions ---
info() {
  printf "[INFO] %s\\n" "$1"
}

error_exit() {
  printf "[ERROR] %s\\n" "$1" >&2
  exit 1
}

# --- Check Prerequisites ---
if ! command -v curl >/dev/null && ! command -v wget >/dev/null; then
  error_exit "Please install curl or wget to download the release."
fi
if ! command -v tar >/dev/null; then
  error_exit "Please install tar to extract the release."
fi
if ! command -v uname >/dev/null; then
  error_exit "Cannot determine OS or architecture (uname not found)."
fi

# --- Detect OS/Arch ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
TARGET_ARCH=""

case $ARCH in
  x86_64 | amd64)
    TARGET_ARCH="amd64"
    ;;
  arm64 | aarch64)
    TARGET_ARCH="arm64"
    ;;
  *)
    error_exit "Unsupported architecture: $ARCH"
    ;;
esac

case $OS in
  linux)
    OS="linux"
    ;;
  *)
    error_exit "Unsupported operating system: $OS (This script currently supports Linux only)"
    ;;
esac

info "Detected OS: $OS, Arch: $TARGET_ARCH"

# --- Fetch Latest Release Info ---
info "Fetching latest release information from GitHub..."
API_URL="https://api.github.com/repos/$REPO/releases/latest"
DOWNLOAD_URL=""
LATEST_VERSION=""

if command -v curl >/dev/null; then
  API_RESPONSE=$(curl -sSL --fail "$API_URL") || error_exit "Failed to fetch release info from GitHub API (curl). Check network or repository URL."
else
  API_RESPONSE=$(wget -qO- "$API_URL") || error_exit "Failed to fetch release info from GitHub API (wget). Check network or repository URL."
fi

if command -v jq >/dev/null; then
    LATEST_VERSION=$(echo "$API_RESPONSE" | jq -r ".tag_name // \"\"")
    ASSET_PATTERN="_${OS}_${TARGET_ARCH}.tar.gz"
    DOWNLOAD_URL=$(echo "$API_RESPONSE" | jq -r ".assets[] | select(.name | endswith(\"$ASSET_PATTERN\")) | .browser_download_url // \"\"")
else
    info "jq not found, attempting less reliable grep/sed fallback..."
    LATEST_VERSION=$(echo "$API_RESPONSE" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | head -n 1)
    if [ -z "$LATEST_VERSION" ]; then error_exit "Could not parse latest version tag from GitHub API response."; fi

    PROJECT_NAME=$(basename "$REPO") # Should be 'reflow'
    EXPECTED_FILENAME="${PROJECT_NAME}_${LATEST_VERSION}_${OS}_${TARGET_ARCH}.tar.gz"
    info "Looking for asset: $EXPECTED_FILENAME"
    DOWNLOAD_URL=$(echo "$API_RESPONSE" | grep '"browser_download_url":' | grep "$EXPECTED_FILENAME" | sed -E 's/.*"([^"]+)".*/\1/' | head -n 1)
fi

if [ -z "$LATEST_VERSION" ] || [ "$LATEST_VERSION" = "null" ]; then
    error_exit "Could not determine latest version tag from GitHub API."
fi
if [ -z "$DOWNLOAD_URL" ] || [ "$DOWNLOAD_URL" = "null" ]; then
  error_exit "Could not find suitable release asset URL for $OS/$TARGET_ARCH (Version: $LATEST_VERSION). Please check the releases page: https://github.com/$REPO/releases"
fi

info "Latest version: $LATEST_VERSION"
info "Downloading asset: $DOWNLOAD_URL"

# --- Download and Extract ---
FILENAME=$(basename "$DOWNLOAD_URL")
TMP_DIR=$(mktemp -d -t reflow_install.XXXXXX)
trap 'info "Cleaning up temporary directory: $TMP_DIR"; rm -rf "$TMP_DIR"' EXIT
DOWNLOAD_PATH="$TMP_DIR/$FILENAME"
EXTRACTED_BINARY="$TMP_DIR/$BINARY_NAME"

if command -v curl >/dev/null; then
  curl -sSL -o "$DOWNLOAD_PATH" "$DOWNLOAD_URL" || error_exit "Download failed (curl)."
else
  wget -q -O "$DOWNLOAD_PATH" "$DOWNLOAD_URL" || error_exit "Download failed (wget)."
fi
info "Downloaded to $DOWNLOAD_PATH"

info "Extracting $BINARY_NAME..."
tar -xzf "$DOWNLOAD_PATH" -C "$TMP_DIR" --strip-components=0 "$BINARY_NAME" || \
tar -xzf "$DOWNLOAD_PATH" -C "$TMP_DIR" --strip-components=1 "$BINARY_NAME" || \
error_exit "Failed to extract $BINARY_NAME from archive. Archive structure might have changed."

if [ ! -f "$EXTRACTED_BINARY" ]; then
  error_exit "$BINARY_NAME not found after extraction."
fi

# --- Install ---
info "Making binary executable..."
chmod +x "$EXTRACTED_BINARY"

info "Attempting to install $BINARY_NAME to $INSTALL_DIR..."
if [ ! -d "$INSTALL_DIR" ]; then
    info "Creating install directory $INSTALL_DIR (may require sudo)..."
    if [ "$(id -u)" -ne 0 ]; then
        sudo mkdir -p "$INSTALL_DIR" || error_exit "Failed to create $INSTALL_DIR using sudo."
    else
        mkdir -p "$INSTALL_DIR" || error_exit "Failed to create $INSTALL_DIR."
    fi
fi

if [ "$(id -u)" -ne 0 ]; then
  if ! command -v sudo >/dev/null; then
    error_exit "sudo command not found, cannot install to $INSTALL_DIR. Please run script with sudo or install manually: mv $EXTRACTED_BINARY $INSTALL_DIR/"
  fi
  info "Running 'sudo mv' to install..."
  sudo mv "$EXTRACTED_BINARY" "$INSTALL_DIR/$BINARY_NAME" || error_exit "Failed to move binary to $INSTALL_DIR using sudo. Check permissions or run script with 'sudo bash'."
else
  info "Running 'mv' as root to install..."
  mv "$EXTRACTED_BINARY" "$INSTALL_DIR/$BINARY_NAME" || error_exit "Failed to move binary to $INSTALL_DIR."
fi

info ""
info "$BINARY_NAME version $LATEST_VERSION installed successfully to $INSTALL_DIR/$BINARY_NAME"
info "Run '$BINARY_NAME --help' to get started."

exit 0