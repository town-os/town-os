#!/usr/bin/env bash
set -e
. make/lib.sh

case "$1" in
  start)
    step "Starting Gitea"
    # Start a local Gitea container and create the admin user.
    remove_container "${GITEA_CONTAINER}"
    ${SUDO} podman load -i "${IMAGE_CACHE}/gitea-latest.tar"
    ${SUDO} podman run -d --pull=never --name "${GITEA_CONTAINER}" \
      -p "$(cat .gitea-port):3000" \
      -e GITEA__security__INSTALL_LOCK=true \
      docker.io/gitea/gitea:latest
    substep "Waiting for Gitea to be ready"
    wait_for_url "http://127.0.0.1:$(cat .gitea-port)/api/v1/version" 60
    substep "Creating Gitea admin user"
    ${SUDO} podman exec --user git "${GITEA_CONTAINER}" \
      gitea admin user create --admin \
      --username town-os --password town-os-test \
      --email town-os@localhost --must-change-password=false 2>/dev/null || true
    substep "Gitea running on port $(cat .gitea-port)"
    ;;
  populate)
    step "Populating Gitea with test repos"
    # Populate Gitea with test repos cached from GitHub and pushed via go-git.
    mkdir -p .cache/git-repos
    GITEA_URL="http://127.0.0.1:$(cat .gitea-port)" \
    GIT_CACHE_DIR=.cache/git-repos \
      go run ./src/gitea/cmd/populate-repos/
    ;;
  stop)
    step "Stopping Gitea"
    # Stop and remove the local Gitea container.
    remove_container "${GITEA_CONTAINER}"
    rm -f .gitea-port
    ;;
  *)
    echo "Usage: $0 {start|populate|stop}"
    exit 1
    ;;
esac
