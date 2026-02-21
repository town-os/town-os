FROM golang:1.25-bookworm AS go-deps
RUN apt-get update && apt-get install -y libsystemd-dev

FROM go-deps AS go-builder
COPY go.mod go.sum /src/
WORKDIR /src
RUN go mod download
COPY . /src
RUN CGO_ENABLED=1 go build -o /systemcontroller ./src/svc/systemcontroller/cmd/systemcontroller
RUN CGO_ENABLED=0 go build -o /town-os-upnp ./src/upnp/cmd/town-os-upnp

FROM oven/bun:latest AS ui-builder
COPY ui/package.json ui/bun.lock /ui/
WORKDIR /ui
RUN bun install --frozen-lockfile
COPY ui/ /ui/
RUN bun run build

FROM debian:bookworm-slim AS runtime-deps
RUN apt-get update && apt-get install -y \
    btrfs-progs libsystemd0 podman \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

FROM runtime-deps
COPY --from=go-builder /systemcontroller /systemcontroller
COPY --from=go-builder /town-os-upnp /town-os-upnp
COPY --from=ui-builder /ui/dist /ui
EXPOSE 5309
CMD ["/systemcontroller"]
