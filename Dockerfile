# syntax=docker/dockerfile:1

# Build stage runs on the build host's native platform and cross-compiles,
# so multi-arch builds (linux/amd64, linux/arm64) need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=""
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .

RUN set -eu; \
    pkg=github.com/Seungjun1127/HelloCalc/internal/buildinfo; \
    ldflags="-s -w -X ${pkg}.Commit=${COMMIT} -X ${pkg}.BuildTime=${BUILD_TIME}"; \
    if [ -n "${VERSION}" ]; then ldflags="${ldflags} -X ${pkg}.Version=${VERSION}"; fi; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
      go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/hellocalc ./cmd/hellocalc

# Runtime stage: static distroless image (no shell, no package manager),
# running as the unprivileged "nonroot" user (UID/GID 65532).
FROM gcr.io/distroless/static-debian13:nonroot

ARG VERSION=""
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
LABEL org.opencontainers.image.title="HelloCalc" \
      org.opencontainers.image.description="A tiny reference workload." \
      org.opencontainers.image.source="https://github.com/Seungjun1127/HelloCalc" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

COPY --from=build /out/hellocalc /hellocalc

ENV HOST=0.0.0.0 \
    PORT=8080 \
    LOG_LEVEL=info
USER 65532:65532
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/hellocalc", "healthcheck"]

# Exec form: hellocalc is PID 1 and receives SIGTERM/SIGINT directly.
ENTRYPOINT ["/hellocalc"]
