FROM golang:1.25-bookworm AS go-builder

COPY go.mod go.sum /src/
WORKDIR /src

RUN go mod download

COPY . /src

RUN go build -o /testserver ./src/svc/systemcontroller/cmd/testserver

FROM oven/bun:latest AS ui-builder

COPY ui/package.json ui/bun.lock /ui/
WORKDIR /ui

RUN bun install --frozen-lockfile

COPY ui/ /ui/

RUN bun run build

FROM debian:bookworm-slim

COPY --from=go-builder /testserver /testserver
COPY --from=ui-builder /ui/dist /ui

EXPOSE 8080

CMD ["/testserver", "-static", "/ui"]
