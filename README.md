[![Town OS](./banner.png)](https://town-os.github.io)

**Home: <https://town-os.github.io>** | [Source (Gitea)](https://gitea.com/town-os/town-os) | [Mirror (GitHub)](https://github.com/town-os/town-os)

> **Your Cloud in Your Closet, easy enough for anyone.**

Town OS is a self-service platform that runs entirely from a USB drive, turning any computer into a personal cloud server. It manages its own storage, networking, and services in containers -- no installation required. Designed for anyone, not just experts: a friendly UI guides you through everything, so you don't have to know how any of this works unless you want to.

**GITHUB USERS:** Please note the source repository is <https://gitea.com/town-os/town-os> -- the package repositories are kept here for simplicity's sake. Issues and Pull Requests must be filed there, as well as any other interactivity with the repository. This repository may also occasionally be out of data or missing other data the Gitea repository has. Do not rely on it.

_([Wondering about the insane patch velocity with high quality? I'm doing it with a $200/mo Claude Account](https://github.com/erikh/hydra))_

## Table of Contents

- [Why It Matters](#why-it-matters)
- [Existing and Planned Features](#existing-and-planned-features)
- [Requests for Comment](#requests-for-comment)
- [Prerequisites](#prerequisites)
- [Development](#development)
- [Makefile Targets](#makefile-targets)
  - [Testing](#testing)
  - [Local Registry](#local-registry)
  - [Local Gitea Server](#local-gitea-server)
  - [Building](#building)
  - [Btrfs Management](#btrfs-management)
  - [Cleanup](#cleanup)
  - [Linting](#linting)
- [License](#license)
- [From](#from)

## Why It Matters

Your data should live in your home, not on someone else's computer. Cloud services are convenient, but they come with monthly fees, privacy trade-offs, and the risk that a company can change terms, raise prices, or shut down at any time. Town OS gives you the same convenience without giving up control.

You don't need to be technical to use it. Plug in a USB drive, power on, and you have a working system. Upgrade by swapping the USB drive. Reset by rebooting. If something goes wrong, you can always get back to a working state -- you can't lock yourself out.

Town OS is built so that anyone can help their family run services at home. Set up a media server for your parents, keep your kids' devices free of spyware, or host your own website -- all without asking permission from a cloud provider.

## Existing and Planned Features

Packaging is fully integrated with the storage and network, creating resources on demand, including opening ports over UPnP and managing port forwarding via a per-package network controller, or establishing tunnels. This functionality is coming soon, but router-level functionality is expected to arrive which would allow users more control over DNS and DHCP within their home and direct network-mappable relationships with functionality to block internet traffic for children, or ad-ware, or anything else. Providing a local resolver that can be programmed by Town OS allows for this and also package integrations like subdomain names within a private network. Slices can be torn off to provide for wireguard networks as well.

The storage system is designed alongside the packaging system to support upgrading and also temporary uninstallation and later restoration. Storage is uniquely partitioned for packages allowing for DR strategies which can be prioritized based on cost and availability. Quotas are used to keep storage needs from surprising the user.

Packages are able to request input from the user -- similar to debconf -- but through the UI (look at the screen shots). These are template variables and can be used to configure container images and manage networking. Packages can also define Go `text/template` file templates that are rendered into volumes at install time, with access to user responses, package metadata, and system information (hostname, IPs). Templates are applied after volume seeding but before service boot, and existing files are never overwritten. [The package repository](https://github.com/town-os/default-packages) has more information. You can also completely replace the repositories with a private repository list -- perfect for your gamer buddies, family members you need to support, etc. A lot of expansion is expected here.

Services all have adequate logging and supervision. There is a comfortable UI for accessing this information, presented in a way that is intended to be safe for non-technical users to consume. There are separate accounts for admin and normal users: you could help your parents run a Plex (or something similar) if you wanted. You could keep them spyware free.

The entire interface is internationalized. All user-facing strings -- both backend error messages and frontend UI text -- are routed through a message catalog keyed by BCP 47 locale codes (e.g. `en-US`, `de-DE`). A language selection screen in System Settings presents 21 common languages in their native scripts, with an expandable list of 87+ country-specific codes. Only English (en-US) is fully translated today; the infrastructure is ready for community translations.

A built-in monitoring stack provides system observability out of the box. Prometheus collects metrics, Node Exporter reports host-level statistics, and Grafana serves auto-provisioned dashboards -- all managed as a single unit with no manual configuration required.

QEMU virtual machine support runs alongside containers as a first-class runtime. Packages can declare `runtime_type: vm` with a QEMU disk image, memory, and CPU count. The Control Plane Service downloads images from URLs, converts them to raw format with `qemu-img`, and caches them in a `vm-images` btrfs subvolume. At install time, a systemd service unit launches `qemu-system-x86_64` with KVM acceleration, virtio networking, and user-mode port forwarding. VM images can be listed, uploaded, and deleted through the API and UI.

Static pages hosting lets you publish HTML content directly from the UI. Three source types are supported: upload a tar archive (the default), extract files from a container image, or clone a git repository. A dropdown in the create dialog selects the source type, and each page is served through a Caddy reverse proxy with its own domain. Archive pages can be updated by uploading a new archive at any time; git and container image pages can be rebuilt on demand to pull the latest content.

Check out some of the [screen shots](./screenshots/). This all works in the dev tasks today.

For detailed usage instructions, including the packaging system, storage, pages, and API documentation, visit **<https://town-os.github.io>**.

## Requests for Comment

Please the try the development build (`make dev` on any linux; see below for more) and add [issues](https://gitea.com/town-os/town-os/issues) (Gitea account required; GitHub SSO can be used) with features you'd like. I'm trying to be very receptive and open-minded to all possibilities, so please do not feel like your idea is too big or too crazy. Just post it. <3

## Prerequisites

- Go 1.25+
- [Bun](https://bun.sh) (JS runtime)
- Podman (rootful, with `sudo`) and `runc` runtime
- QEMU (`qemu-system-x86_64`, `qemu-img`) for VM package support
- btrfs-progs (`mkfs.btrfs`)
- libsystemd (development headers for systemd integration)
- golangci-lint
- Python 3 (used for port allocation in test targets)

### Ubuntu

You can install all dependencies at once using the provided script ([`install-ubuntu-deps.sh`](./install-ubuntu-deps.sh)):

```bash
./install-ubuntu-deps.sh
exec $SHELL
```

After the script completes, `exec $SHELL` reloads your shell so that newly installed tools (like `bun` and `golangci-lint`) are available on your `PATH`.

Or install them manually:

```bash
sudo apt-get update
sudo apt-get install -y golang btrfs-progs libsystemd-dev podman runc python3 unzip build-essential qemu-system-x86 qemu-utils
```

Install [Bun](https://bun.sh):

```bash
curl -fsSL https://bun.sh/install | bash
```

Install [golangci-lint](https://golangci-lint.run/welcome/install/):

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin
```

### Arch Linux / Manjaro

```bash
sudo pacman -S go podman runc btrfs-progs python qemu-full
```

Install [Bun](https://bun.sh):

```bash
curl -fsSL https://bun.sh/install | bash
```

Install [golangci-lint](https://golangci-lint.run/welcome/install/):

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin
```

Optionally create a `.env` file with repository credentials for the **dev environment**:

```
TOWN_OS_REPO_USERNAME=<username>
TOWN_OS_REPO_PASSWORD=<password>
```

These are used by `make dev` to set default repository credentials on the backend. The password may be an HTTP API key from Gitea or GitHub. If omitted, no default credentials are set and only public repositories can be added without explicit credentials.

Integration tests (`make test-full`) do **not** use `.env` credentials. They run a local Gitea instance with hardcoded test credentials (`town-os` / `town-os-test`) and never contact GitHub for repository operations.

After installing prerequisites, run `make pull-images` once to fetch all container images from Docker Hub and save them to the global cache at `/var/cache/town-os/images/`. Docker Hub credentials (via `DOCKER_USERNAME` / `DOCKER_PASSWORD` in `.env`) are only needed if you hit rate limits. All other build and test targets load images from the global cache and never contact Docker Hub. If any cached image is missing when a target needs it, `make pull-images` runs automatically.

## Development

**There are probably still some outstanding issues running integration tests while the dev server is running. This is being investigated.**

If you're just trying things out, use the `stable` branch (the default). If you want the latest changes (which may not be good), use `main`. Both branches will roll as things are deemed stable or integrated into the repository.

Run `make dev` to build the test image, create a dev btrfs volume, start the backend container on port 5309, and launch the Vite dev server with hot reload. Once running, access the UI at `http://<hostname>:5173`.

Ports 8080 (backend API) and 5173 (Vite dev server) must be accessible on the host.

| Target           | Description                                                                     |
| ---------------- | ------------------------------------------------------------------------------- |
| `make dev`       | Start the full dev environment (backend + Vite dev server).                     |
| `make dev-stop`  | Stop and remove the dev backend container.                                      |
| `make dev-logs`  | Tail journalctl inside the running dev container.                               |
| `make clean-dev` | Stop the dev container and tear down the dev btrfs volume. Removes `dev-data/`. |

## Makefile Targets

### Testing

| Target                     | Description                                                                                                             |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `make test`                | Run lint, Go unit tests, and JS unit tests.                                                                             |
| `make test-integration`    | Run Go integration tests inside a privileged podman container with systemd and btrfs. Cleans up btrfs loopback on exit. |
| `make test-ui-integration` | Run JS (bun) UI integration tests against a backend container. Cleans up btrfs loopback on exit.                        |
| `make test-full`           | Run `test`, `test-integration`, and `test-ui-integration` in sequence. Uses a signal trap for guaranteed cleanup.       |
| `make test-systemd`        | Run only the systemd-related integration tests (`TestPodman`).                                                          |
| `make auto-test`           | Watch for `.go`/`.js` file changes and re-run `make test` automatically. Reflex is automatically installed when needed. |
| `make auto-test-full`      | Watch for file changes and re-run `make test-full` automatically. Reflex is automatically installed when needed.        |

### Local Registry

Integration tests use a local `registry:2` container to avoid Docker Hub rate limits. When you run `make test-integration` or `make test-ui-integration`, the build automatically:

1. Discovers all `docker.io` images referenced by test package repositories (`discover-images` tool)
2. Starts a local registry on a random port
3. Loads each image from the global cache and pushes it to the local registry
4. Generates a `registries.conf` that redirects `docker.io` pulls to the local mirror
5. Mounts the config into the test container

This is transparent -- no code changes are needed. All images are loaded from the global cache (`/var/cache/town-os/images/`); no docker.io pulls occur during the test pipeline.

| Target                   | Description                                               |
| ------------------------ | --------------------------------------------------------- |
| `make registry`          | Start the local registry container.                       |
| `make registry-populate` | Mirror discovered docker.io images to the local registry. |
| `make registry-stop`     | Stop and remove the local registry container.             |

Each working directory gets its own registry instance (via `INSTANCE_ID`), so concurrent test runs do not conflict.

### Local Gitea Server

Integration tests also use a local Gitea instance to avoid cloning test package repositories directly from GitHub. When you run `make test-integration` or `make test-ui-integration`, the build automatically:

1. Starts a local Gitea server on a random port
2. Creates an admin user
3. Caches test package repositories as bare clones in `.cache/git-repos/` (fetches to refresh on subsequent runs)
4. Pushes cached repos into Gitea via go-git (`populate-repos` tool)
5. Passes `TOWN_OS_TEST_REPO_CORE_URL` and `TOWN_OS_TEST_REPO_EXTRAS_URL` env vars into the test container so all git clones hit the local Gitea instance

The bare clone cache in `.cache/git-repos/` persists across Gitea restarts, so only the first run hits GitHub. Subsequent runs fetch updates and push to the fresh Gitea instance. `make clean` removes the cache.

This is transparent -- no code changes are needed. Tests fall back to GitHub URLs if the env vars are not set.

| Target                | Description                                                |
| --------------------- | ---------------------------------------------------------- |
| `make gitea`          | Start the local Gitea container and create the admin user. |
| `make gitea-populate` | Migrate test repos from GitHub into the local Gitea.       |
| `make gitea-stop`     | Stop and remove the local Gitea container.                 |

Each working directory gets its own Gitea instance (via `INSTANCE_ID`), so concurrent test runs do not conflict.

### Building

| Target                       | Description                                                        |
| ---------------------------- | ------------------------------------------------------------------ |
| `make production-image`      | Build the production base image (for integration tests).           |
| `make dev-production-image`  | Build the production base image (for dev).                         |
| `make test-image`            | Build the test container image (includes integration test binary). |
| `make dev-image`             | Build the dev container image.                                     |
| `make ui-integration-image`  | Build the UI integration test container image.                     |
| `make pull-images`           | Pull all container images from Docker Hub and save to global cache. Runs automatically if any cached image is missing. |

Dev and integration use separate production base images and build caches so concurrent builds cannot interfere with each other.

### Btrfs Management

| Target                 | Description                                                    |
| ---------------------- | -------------------------------------------------------------- |
| `make btrfs`           | Create a 50GB btrfs loopback volume for integration tests.     |
| `make clean-btrfs`     | Unmount, detach loop devices, and remove the integration test btrfs volume. |
| `make btrfs-dev`       | Create a 50GB btrfs loopback volume for the dev environment.                |
| `make clean-btrfs-dev` | Unmount, detach loop devices, and remove the dev btrfs volume.              |
| `make dev-btrfs`       | Create the dev btrfs volume only if one isn't already mounted. |

The dev and integration test environments use separate btrfs volumes, container images, and build caches so they can run concurrently without conflict.

### Cleanup

`make test-full` automatically cleans up all integration containers, registry, Gitea, and btrfs loopback volumes after tests complete. A shell EXIT trap ensures cleanup runs even when interrupted by signals. Each integration test target (`test-integration`, `test-ui-integration`) also uses its own EXIT trap to guarantee btrfs loopback cleanup regardless of how the recipe terminates (success, failure, or signal interruption). The `clean-btrfs` target includes a safety net that scans for orphaned loop devices backed by btrfs images in the current directory, handling cases where tracking files are missing.

| Target                   | Description                                                              |
| ------------------------ | ------------------------------------------------------------------------ |
| `make clean`             | Clean dev resources (containers, btrfs, dev-data, dev-repos) and caches. |
| `make clean-dev`         | Stop all dev containers, tear down dev btrfs, remove dev-data/dev-repos. |
| `make clean-cache`       | Same as `clean-dev` (used as a dependency by `clean`).                   |
| `make clean-integration` | Remove only the integration test containers and port file.               |
| `make clean-btrfs`       | Unmount and remove the integration test btrfs volume and orphaned loop devices. |
| `make clean-containers`  | Remove all town-os containers from any working directory / instance.     |
| `make clean-all`         | Clean everything: all containers, dev, integration, and btrfs.           |

### Linting

| Target      | Description                       |
| ----------- | --------------------------------- |
| `make lint` | Run `go vet` and `golangci-lint`. |

## License

GNU Affero GPL 3.0

## From

Town OS is not a one-man show -- it's supposed to be here for everyone to contribute ideas to. A solution that's free isn't really free if it's just done by one person. The licensing is deliberately chosen to ensure that people can audit, fix, and contribute back to a product where you know what you're getting on the label.

[Erik Hollensbe](mailto:erik@hollensbe.org) conceived this project. Several people have already made significant financial contributions to keep me housed and living fairly well, considering I'm in the Bay Area. Lots of people contributed lots and lots of ideas. And Claude helped too. This is as much from them as it is from me.
