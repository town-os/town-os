FROM golang:1.25-bookworm AS go-deps
RUN apt-get update && apt-get install -y libsystemd-dev

FROM go-deps AS go-builder
COPY go.mod go.sum /src/
WORKDIR /src
RUN go mod download
COPY . /src
ARG TOWN_OS_TAG=rc.latest
# TOWN_OS_GO_TAGS is the comma-separated list of Go build tags forwarded by
# the make pipeline. Empty by default; PROTON_ENABLED=1 sets it to "proton".
ARG TOWN_OS_GO_TAGS=
RUN CGO_ENABLED=1 go build -tags "${TOWN_OS_GO_TAGS}" -ldflags "-s -w -X main.Version=${TOWN_OS_TAG}" -o /systemcontroller ./src/svc/systemcontroller/cmd/systemcontroller
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller

FROM oven/bun:latest AS ui-builder
COPY ui/package.json ui/bun.lock /ui/
WORKDIR /ui
RUN bun install --frozen-lockfile
COPY ui/ /ui/
RUN bun run build

FROM debian:bookworm-slim AS runtime-deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    btrfs-progs libsystemd0 podman runc socat \
    pigz lbzip2 xz-utils \
    ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN printf '[engine]\nruntime = "runc"\n' > /etc/containers/containers.conf

FROM runtime-deps
ARG TOWN_OS_TAG=rc.latest
RUN echo "${TOWN_OS_TAG}" > /town-os.tag
COPY --from=go-builder /systemcontroller /systemcontroller
COPY --from=go-builder /town-os-networkcontroller /town-os-networkcontroller
COPY --from=ui-builder /ui/dist /ui
EXPOSE 5309
CMD ["/systemcontroller"]
