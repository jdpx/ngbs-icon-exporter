# Self-contained multi-stage build (works with a plain `docker build .`).
# Releases published by CI use the prebuilt binary via Dockerfile.goreleaser.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /ngbs-icon-exporter .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /ngbs-icon-exporter /ngbs-icon-exporter
EXPOSE 9924
USER nonroot:nonroot
ENTRYPOINT ["/ngbs-icon-exporter"]
