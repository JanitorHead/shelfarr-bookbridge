# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bookbridge ./cmd/bookbridge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bookbridge /usr/local/bin/bookbridge
ENV BB_DB=/config/bookbridge.db
VOLUME ["/config"]
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/bookbridge"]
CMD ["daemon"]
