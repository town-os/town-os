#!/usr/bin/env bash
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

case "$1" in
  start)
    step "Starting Gitea"
    # Start a local Gitea container and create the admin user.
    remove_container "${GITEA_CONTAINER}"
    ${SUDO} podman load -i "${IMAGE_CACHE}/gitea-latest.tar"
    # --replace: ensure concurrent make test-full runs never conflict on container names
    ${SUDO} podman run -d --pull=never --replace --name "${GITEA_CONTAINER}" \
      -p "$(cat "${STATE_DIR}/.gitea-port"):3000" \
      -e GITEA__security__INSTALL_LOCK=true \
      docker.io/gitea/gitea:latest
    substep "Waiting for Gitea to be ready"
    wait_for_url "http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")/api/v1/version" 60
    substep "Creating Gitea admin user"
    ${SUDO} podman exec --user git "${GITEA_CONTAINER}" \
      gitea admin user create --admin \
      --username town-os --password town-os-test \
      --email town-os@localhost --must-change-password=false 2>/dev/null || true
    substep "Gitea running on port $(cat "${STATE_DIR}/.gitea-port")"
    ;;
  populate)
    step "Populating Gitea with test repos"
    warn_missing_repo_creds
    # Populate Gitea with test repos cached from GitHub and pushed via go-git.
    mkdir -p "${STATE_DIR}/git-repos"
    GITEA_URL="http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")" \
    GIT_CACHE_DIR="${STATE_DIR}/git-repos" \
      go run ./src/gitea/cmd/populate-repos/
    ;;
  stop)
    step "Stopping Gitea"
    # Stop and remove the local Gitea container.
    remove_container "${GITEA_CONTAINER}"
    rm -f "${STATE_DIR}/.gitea-port"
    ;;
  *)
    echo "Usage: $0 {start|populate|stop}"
    exit 1
    ;;
esac
