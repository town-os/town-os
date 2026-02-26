[![Town OS](./banner.png)](https://town-os.github.io)

**Home: <https://town-os.github.io>** | [Source (Gitea)](https://gitea.com/town-os/town-os) | [Mirror (GitHub)](https://github.com/town-os/town-os)

> **Your Cloud in Your Closet, easy enough for anyone.**

**GITHUB USERS:** Please note the source repository is <https://gitea.com/town-os/town-os> -- the package repositories are kept here for simplicity's sake. Issues and Pull Requests must be filed there, as well as any other interactivity with the repository. This repository may also occasionally be out of data or missing other data the Gitea repository has. Do not rely on it.

The goal of this system is to build a self-service platform that anyone can run at home, with premier ease of use experience and reliability acceptable for a home user.

Town OS is never installed: it lives on a USB drive and runs entirely in memory. It uses all the storage in your computer for **your stuff**. Upgrade Town OS by powering off and replacing a USB drive, or reset it to a default state after you made a boo-boo by rebooting it.

This platform manages its own storage, state, and is completely responsible for its own health. A USB drive running squashfs provides the host operating system in ram, and the services that Town OS has running in containers, pulled from the internet, can manage any changes of state that need to occur over the lifetime of the power cycle. Thus, a reboot can be a simple way to allow users to get themselves to a working state, or a user can upgrade simply by replacing the USB drive it boots from.

Packaging is fully integrated with the storage and network, creating resources on demand, including opening ports over UPnP and managing port forwarding via a per-package network controller, or establishing tunnels. This functionality is coming soon, but router-level functionality is expected to arrive which would allow users more control over DNS and DHCP within their home and direct network-mappable relationships with functionality to block internet traffic for children, or ad-ware, or anything else. Providing a local resolver that can be programmed by Town OS allows for this and also package integrations like subdomain names within a private network. Slices can be torn off to provide for wireguard networks as well.

The storage system is designed alongside the packaging system to support upgrading and also temporary uninstallation and later restoration. Storage is uniquely partitioned for packages allowing for DR strategies which can be prioritized based on cost and availability. Quotas are used to keep storage needs from surprising the user.

Packages are able to request input from the user -- similar to debconf -- but through the UI (look at the screen shots). These are template variables and can be used to configure container images and manage networking. [The package repository](https://github.com/town-os/default-packages) has more information. You can also completely replace the repositories with a private repository list -- perfect for your gamer buddies, family members you need to support, etc. A lot of expansion is expected here.

Services all have adequate logging and supervision. There is a comfortable UI for accessing this information, presented in a way that is intended to be safe for non-technical users to consume. There are separate accounts for admin and normal users: you could help your parents run a Plex (or something similar) if you wanted. You could keep them spyware free.

You also can't lock yourself out. If all accounts become disabled or there are none... it runs behind the firewall. Just create a new one and fix it. Or, if you really get yourself into a bad spot, you can actually nuke the entire SQLite database to recover a system. The important storage is all kept in atomically-managed JSON files or is actually the system itself.

Check out some of the [screen shots](./screenshots/). This all works in the dev tasks today.

## Requests for Comment

Please the try the development build (`make dev` on any linux; see below for more) and add [issues](https://gitea.com/town-os/town-os/issues) (Gitea account required; GitHub SSO can be used) with features you'd like. I'm trying to be very receptive and open-minded to all possibilities, so please do not feel like your idea is too big or too crazy. Just post it. <3

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
| `make test-integration`    | Run Go integration tests inside a privileged podman container with systemd and btrfs.                                   |
| `make test-ui-integration` | Run JS (bun) UI integration tests against a backend container.                                                          |
| `make test-full`           | Run `test`, `test-integration`, and `test-ui-integration` in sequence.                                                  |
| `make test-systemd`        | Run only the systemd-related integration tests (`TestPodman`).                                                          |
| `make auto-test`           | Watch for `.go`/`.js` file changes and re-run `make test` automatically. Reflex is automatically installed when needed. |
| `make auto-test-full`      | Watch for file changes and re-run `make test-full` automatically. Reflex is automatically installed when needed.        |

### Building

| Target                       | Description                                                        |
| ---------------------------- | ------------------------------------------------------------------ |
| `make production-image`      | Build the production base image (for integration tests).           |
| `make dev-production-image`  | Build the production base image (for dev).                         |
| `make test-image`            | Build the test container image (includes integration test binary). |
| `make dev-image`             | Build the dev container image.                                     |
| `make ui-integration-image`  | Build the UI integration test container image.                     |
| `make pull-images`           | Pull base container images from Docker Hub.                        |

Dev and integration use separate production base images and build caches so concurrent builds cannot interfere with each other.

### Btrfs Management

| Target                 | Description                                                    |
| ---------------------- | -------------------------------------------------------------- |
| `make btrfs`           | Create a 50GB btrfs loopback volume for integration tests.     |
| `make clean-btrfs`     | Unmount and remove the integration test btrfs volume.          |
| `make btrfs-dev`       | Create a 50GB btrfs loopback volume for the dev environment.   |
| `make clean-btrfs-dev` | Unmount and remove the dev btrfs volume.                       |
| `make dev-btrfs`       | Create the dev btrfs volume only if one isn't already mounted. |

The dev and integration test environments use separate btrfs volumes, container images, and build caches so they can run concurrently without conflict.

### Cleanup

| Target                   | Description                                                              |
| ------------------------ | ------------------------------------------------------------------------ |
| `make clean`             | Clean dev resources (containers, btrfs, dev-data, dev-repos) and caches. |
| `make clean-dev`         | Stop the dev container, tear down dev btrfs, remove dev-data/dev-repos.  |
| `make clean-cache`       | Same as `clean-dev` (used as a dependency by `clean`).                   |
| `make clean-integration` | Remove only the integration test containers and port file.               |
| `make clean-btrfs`       | Unmount and remove the integration test btrfs volume.                    |
| `make clean-all`         | Clean everything: dev, integration containers, and integration btrfs.    |

### Linting

| Target      | Description                       |
| ----------- | --------------------------------- |
| `make lint` | Run `go vet` and `golangci-lint`. |

## Archive Upload / Download

Subvolume data can be populated from archive files or container images, and exported on demand.

### Archive Upload API

`POST /storage/upload-archive` (admin required) accepts a multipart form with:

- `subvolume` -- target subvolume path (must not be a reserved filesystem)
- `archive` -- archive file (supported formats: `.tar.gz`, `.tgz`, `.tar.bz2`, `.tbz2`, `.tar.xz`, `.txz`, `.tar`, `.zip`, `.7z`)

The archive is unpacked into the target subvolume. A restart of the associated service is typically needed afterward. The response includes `{"needs_restart": true}`.

The maximum upload size is controlled by the `max_archive_size` setting (default 20 MB). Unpacking is bounded by the `archive_unpack_timeout` setting (default 120 seconds). Both can be changed via the settings API and accept human-readable byte values (e.g. `100MB`, `1GB`).

### Archive Download API

`POST /storage/download-archive` (admin required) accepts a JSON body:

```json
{
  "subvolumes": ["installed/repo/pkg/1.0/data"],
  "stop_service": "pkg.service"
}
```

Returns a 7z archive of the requested subvolume contents. If `stop_service` is set, the unit is stopped before archiving and restarted afterward.

### Auto-Archive from Container Images

Package definitions can include an `archives` section to automatically populate volumes from container images at install time:

```yaml
archives:
  - image: docker.io/library/nginx:latest
    directory: /usr/share/nginx/html
    volume: data
```

If the target volume is empty during install or reconcile, `podman` is used to pull the image, create a temporary container, and copy the specified directory into the volume.

### Git Volume Seed

Volumes can specify a `git` field containing a repository URL. During install and reconcile, empty volumes are seeded by cloning the repository:

```yaml
volumes:
  config:
    mountpoint: /etc/myapp
    git: https://github.com/example/default-config.git
```

The URL can be overridden via a question using template substitution:

```yaml
volumes:
  config:
    mountpoint: /etc/myapp
    git: "@configrepo@"
questions:
  configrepo:
    query: "Git repository for default config:"
```

The clone only runs when the target volume is empty, so existing data is never overwritten. Clone failures are logged and skipped (non-fatal).

### Archive Question Type

Packages can prompt users to upload an archive during installation using the `archive` output type in questions:

```yaml
questions:
  backup:
    query: "Upload a backup archive (or type 'skip'):"
    output: archive
```

The response must be non-empty. `skip` is accepted as a valid value.

### Secret Question Type

Packages can define questions that auto-generate cryptographically secure secrets when the user omits a response:

```yaml
questions:
  db_password:
    query: "Database password (leave empty to auto-generate):"
    type: secret
```

When the response is empty or omitted, a 64-character hex string is generated using `crypto/rand` (256 bits of entropy). The value is safe for use in environment variables and shell contexts. Users can override the generated value by providing an explicit response.

### Settings

| Key                      | Default    | Description                          |
| ------------------------ | ---------- | ------------------------------------ |
| `max_archive_size`       | `20971520` | Maximum upload size in bytes (20 MB) |
| `archive_unpack_timeout` | `120`      | Unpack timeout in seconds            |

### License

GNU Affero GPL 3.0

## From

Town OS is not a one-man show -- it's supposed to be here for everyone to contribute ideas to. A solution that's free isn't really free if it's just done by one person. The licensing is deliberately chosen to ensure that people can audit, fix, and contribute back to a product where you know what you're getting on the label.

[Erik Hollensbe](mailto:erik@hollensbe.org) conceived this project. Several people have already made significant financial contributions to keep me housed and living fairly well, considering I'm in the Bay Area. Lots of people contributed lots and lots of ideas. And Claude helped too. This is as much from them as it is from me.
