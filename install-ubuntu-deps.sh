#!/usr/bin/env bash
set -euo pipefail

# Install Town OS development dependencies on Ubuntu/Debian.

echo "Installing system packages..."
sudo apt-get update
sudo apt-get install -y golang btrfs-progs libsystemd-dev podman unzip

echo "Installing Bun..."
curl -fsSL https://bun.sh/install | bash

echo "Installing golangci-lint..."
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b "$(go env GOPATH)/bin"

echo "Done. You may need to restart your shell or source your profile to pick up new PATH entries."
