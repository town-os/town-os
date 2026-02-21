# Town OS

## Prerequisites

- Go 1.25+
- [Bun](https://bun.sh) (JS runtime)
- Podman (rootful, with `sudo`)
- btrfs-progs (`mkfs.btrfs`)
- golangci-lint

Create a `.env` file with repository credentials:

```
TOWN_OS_REPO_USERNAME=<username>
TOWN_OS_REPO_PASSWORD=<password>
```

The username and password will be used for all repository fetches. If it is omitted, none will be used. These values are used both in the dev environment and the integration tests. The password may be a HTTP API key you get from Gitea or Github. The URLs for the test repositories come from github and are public.

After installing prerequisites, run `make pull-images` before any other targets. This fetches the base container images required by all build and test targets.

## Development

Run `make dev` to build the test image, create a dev btrfs volume, start the backend container on port 5309, and launch the Vite dev server with hot reload. Once running, access the UI at `http://<hostname>:5173`.

Ports 8080 (backend API) and 5173 (Vite dev server) must be accessible on the host.

| Target           | Description                                                                     |
| ---------------- | ------------------------------------------------------------------------------- |
| `make dev`       | Start the full dev environment (backend + Vite dev server).                     |
| `make dev-stop`  | Stop and remove the dev backend container.                                      |
| `make dev-logs`  | Tail journalctl inside the running dev container.                               |
| `make dev-clean` | Stop the dev container and tear down the dev btrfs volume. Removes `dev-data/`. |

## Makefile Targets

### Testing

| Target                     | Description                                                                                                             |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `make test`                | Run lint, Go unit tests, and JS unit tests.                                                                             |
| `make test-integration`    | Run Go integration tests inside a privileged podman container with systemd and btrfs.                                   |
| `make test-ui-integration` | Run JS (bun) UI integration tests against a backend container.                                                          |
| `make test-full`           | Run `test`, `test-integration`, and `test-ui-integration` in sequence.                                                  |
| `make test-systemd`        | Run only the systemd-related integration tests (`TestPodman`).                                                          |
| `make auto-test`           | Watch for `.go`/`.js` file changes and re-run `make test` automatically. Reflex is automatically installed when needed. |
| `make auto-test-full`      | Watch for file changes and re-run `make test-full` automatically. Reflex is automatically installed when needed.        |

### Building

| Target                      | Description                                                        |
| --------------------------- | ------------------------------------------------------------------ |
| `make production-image`     | Build the production container image.                              |
| `make test-image`           | Build the test container image (includes integration test binary). |
| `make ui-integration-image` | Build the UI integration test container image.                     |
| `make pull-images`          | Pull base container images from Docker Hub.                        |

### Btrfs Management

| Target                 | Description                                                    |
| ---------------------- | -------------------------------------------------------------- |
| `make btrfs`           | Create a 50GB btrfs loopback volume for integration tests.     |
| `make clean-btrfs`     | Unmount and remove the integration test btrfs volume.          |
| `make btrfs-dev`       | Create a 50GB btrfs loopback volume for the dev environment.   |
| `make clean-btrfs-dev` | Unmount and remove the dev btrfs volume.                       |
| `make dev-btrfs`       | Create the dev btrfs volume only if one isn't already mounted. |

The dev and integration test environments use separate btrfs volumes so they can run concurrently without conflict.

### Cleanup

| Target                   | Description                                                 |
| ------------------------ | ----------------------------------------------------------- |
| `make clean`             | Remove all containers, btrfs volumes, caches, and dev data. |
| `make clean-podman`      | Remove all containers and btrfs volumes.                    |
| `make clean-integration` | Remove only the integration test containers.                |

### Linting

| Target      | Description                       |
| ----------- | --------------------------------- |
| `make lint` | Run `go vet` and `golangci-lint`. |
