FROM docker.io/library/golang:1.25-bookworm AS go-deps
RUN apt-get update && apt-get install -y libsystemd-dev

FROM go-deps AS go-builder
COPY go.mod go.sum /src/
WORKDIR /src
RUN go mod download
COPY . /src
# TOWN_OS_GO_TAGS is the comma-separated list of Go build tags forwarded by
# the make pipeline. Empty by default; PROTON_ENABLED=1 sets it to "proton".
ARG TOWN_OS_GO_TAGS=
# The image tag is no longer baked into the binary. At runtime the controller
# defaults to rc.latest-<arch> and the install build system pins a specific tag
# via the TOWN_OS_TAG env var on the systemcontroller systemd unit.
RUN CGO_ENABLED=1 go build -tags "${TOWN_OS_GO_TAGS}" -ldflags "-s -w" -o /systemcontroller ./src/svc/systemcontroller/cmd/systemcontroller
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller

FROM docker.io/oven/bun:latest AS ui-builder
# Fixed cache path so the make pipeline can mount .cache/bun into the build
# (same pattern as the go-mod/go-build volumes) and bun install stays offline
# once the cache is warm.
ENV BUN_INSTALL_CACHE_DIR=/bun-cache
COPY ui/package.json ui/bun.lock /ui/
WORKDIR /ui
RUN bun install --frozen-lockfile
COPY ui/ /ui/
RUN bun run build

# Runtime base: bookworm-SLIM (~29 MB) rather than full bookworm (~144 MB) —
# ~115 MB off the image for free. slim shares the same apt sources, so every
# package below installs identically; it only omits pre-seeded docs/locales we
# don't use. `tar` (execed by the archive handlers) is an essential package and
# is still present in slim.
#
# This image CANNOT go distroless the way rolodex-dns did: the systemcontroller
# is CGO_ENABLED=1 (needs glibc + libsystemd0) and shells out to real binaries —
# podman (remote client to the host socket), btrfs-progs, socat, tar,
# pigz/lbzip2/xz for archives, and wireguard-tools — none of which exist in a
# distroless base and none of which can be apt-installed there. slim is the
# floor for this one.
#
# wireguard-tools supplies `wg`, which the connected-peers panel execs as
# `wg show <iface> dump` to read live handshake/endpoint/transfer state. Only
# `wg` is needed, not `wg-quick`: the interfaces themselves are brought up by
# generated systemd units that run wg-quick ON THE HOST, never in here. The read
# works because the controller runs with --net host and so shares the host
# network namespace where wg-quick created the device.
FROM docker.io/library/debian:bookworm-slim AS runtime-deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    btrfs-progs libsystemd0 podman runc socat \
    pigz lbzip2 xz-utils \
    ca-certificates \
    wireguard-tools \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
# Keep podman's per-network subnet allocation off 10.64.0.0/10, which town-os
# reserves for WireGuard overlays (src/wireguard/ipam.go). If podman's default
# pools (10.89/16, 10.90/15, 10.96/11, ... all inside 10.64.0.0/10) overlap the
# overlay range, in-range /24s get skipped as they conflict with overlay routes
# and the pool exhausts under load ("could not find free subnet from subnet
# pools"), breaking package container networks. 172.16.0.0/12 avoids the clash.
RUN printf '[engine]\nruntime = "runc"\n\n[network]\ndefault_subnet_pools = [{"base" = "172.16.0.0/12", "size" = 24}]\n' > /etc/containers/containers.conf

FROM runtime-deps
COPY --from=go-builder /systemcontroller /systemcontroller
COPY --from=go-builder /town-os-networkcontroller /town-os-networkcontroller
COPY --from=ui-builder /ui/dist /ui
EXPOSE 5309
CMD ["/systemcontroller"]
