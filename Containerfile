FROM golang:1.25-bookworm AS go-builder

RUN apt-get update && apt-get install -y libsystemd-dev

COPY go.mod go.sum /src/
WORKDIR /src

RUN go mod download

COPY . /src

RUN CGO_ENABLED=1 go build -o /testserver ./src/svc/systemcontroller/cmd/testserver

FROM oven/bun:latest AS ui-builder

COPY ui/package.json ui/bun.lock /ui/
WORKDIR /ui

RUN bun install --frozen-lockfile

COPY ui/ /ui/

RUN bun run build

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    btrfs-progs libsystemd0 \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /testserver /testserver
COPY --from=ui-builder /ui/dist /ui

EXPOSE 8080

CMD ["/testserver"]
