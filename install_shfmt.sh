#!/bin/bash

# 1. Fetch the latest version tag from GitHub API
LATEST_TAG=$(curl -s https://api.github.com/repos/mvdan/sh/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

# 2. Detect OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Standardize architecture names for shfmt binaries
[[ "$ARCH" == "x86_64" ]] && ARCH="amd64"
[[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]] && ARCH="arm64"

FILENAME="shfmt_${LATEST_TAG}_${OS}_${ARCH}"
URL="https://github.com/mvdan/sh/releases/download/${LATEST_TAG}/${FILENAME}"

echo "Downloading $FILENAME from GitHub..."

# 3. Download the binary
curl -L -o shfmt "$URL"
if [ $? -ne 0 ]; then
    echo "Error: Failed to download shfmt."
    exit 1
fi

chmod +x shfmt

# 4. Install to /usr/local/bin (requires sudo)
echo "Installing to /usr/local/bin (sudo password may be required)..."
sudo mv shfmt /usr/local/bin/shfmt

if [ $? -eq 0 ]; then
    echo "Success! shfmt is now installed at /usr/local/bin/shfmt"
    echo "You can now set 'shellformat.path': '/usr/local/bin/shfmt' in your VS Code settings."
else
    echo "Error: Failed to move the binary. Please check your permissions."
    exit 1
fi
