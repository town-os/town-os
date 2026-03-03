#!/usr/bin/env bash
set -e

case "$1" in
  start)
    # Start a local Gitea container and create the admin user.
    sudo -E podman rm -f "${GITEA_CONTAINER}" 2>/dev/null || true
    sudo -E podman load -i "${IMAGE_CACHE}/gitea-latest.tar"
    sudo -E podman run -d --pull=never --name "${GITEA_CONTAINER}" \
      -p "$(cat .gitea-port):3000" \
      -e GITEA__security__INSTALL_LOCK=true \
      docker.io/gitea/gitea:latest
    echo "Waiting for Gitea to be ready..."
    for i in $(seq 1 60); do
      curl -sf "http://127.0.0.1:$(cat .gitea-port)/api/v1/version" >/dev/null 2>&1 && break
      sleep 1
    done
    echo "Creating Gitea admin user..."
    sudo -E podman exec --user git "${GITEA_CONTAINER}" \
      gitea admin user create --admin \
      --username town-os --password town-os-test \
      --email town-os@localhost --must-change-password=false 2>/dev/null || true
    echo "Gitea running on port $(cat .gitea-port)"
    ;;
  populate)
    # Populate Gitea with test repos cached from GitHub and pushed via go-git.
    mkdir -p .cache/git-repos
    GITEA_URL="http://127.0.0.1:$(cat .gitea-port)" \
    GIT_CACHE_DIR=.cache/git-repos \
      go run ./src/gitea/cmd/populate-repos/
    ;;
  stop)
    # Stop and remove the local Gitea container.
    sudo -E podman rm -f "${GITEA_CONTAINER}" 2>/dev/null || true
    rm -f .gitea-port
    ;;
  *)
    echo "Usage: $0 {start|populate|stop}"
    exit 1
    ;;
esac
