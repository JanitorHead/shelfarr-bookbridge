# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bookbridge ./cmd/bookbridge

# Runs as root (UID 0): the standard for Unraid, where /config is bind-mounted
# from /mnt/user/appdata (root-owned) — a fixed non-root UID can't write there
# and SQLite fails with "unable to open database file (14)".
FROM gcr.io/distroless/static-debian12:latest
COPY --from=build /out/bookbridge /usr/local/bin/bookbridge
ENV BB_DB=/config/bookbridge.db
VOLUME ["/config"]
EXPOSE 7373
ENTRYPOINT ["/usr/local/bin/bookbridge"]
CMD ["daemon"]
