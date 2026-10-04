FROM --platform=$BUILDPLATFORM oven/bun:1.4.2@sha256:9114c058aeae42162ee16dd5084b95fe9473970bb6bcb5b232ab1630f0546895 AS frontend
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile
COPY web/ ./
COPY scripts/install-linux.sh /src/scripts/install-linux.sh
RUN bun run build

FROM --platform=$BUILDPLATFORM golang:1.27.1@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/handler.go ./web/handler.go
COPY --from=frontend /src/web/dist ./web/dist
COPY agent/scripts/third-party-notices.sh /tools/third-party-notices.sh
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=4 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -p=4 -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" -o /out/arveld ./cmd/arveld
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=4 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    bash /tools/third-party-notices.sh /out/THIRD_PARTY_NOTICES.txt ./cmd/arveld web/dist/THIRD_PARTY_NOTICES.txt /usr/share/doc/ca-certificates/copyright
RUN mkdir -p /out/state

# The exported executable and runtime image share the same build output.
FROM scratch AS binary
COPY --from=build /out/arveld /arveld
COPY --from=build /out/THIRD_PARTY_NOTICES.txt /THIRD_PARTY_NOTICES.txt
COPY LICENSE NOTICE /

FROM scratch AS runtime
ARG VERSION=dev
ARG REVISION
LABEL org.opencontainers.image.source="https://github.com/RSTCK-Innovation/arveld" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=binary /arveld /arveld
COPY --from=binary /LICENSE /NOTICE /THIRD_PARTY_NOTICES.txt /usr/share/licenses/arveld/
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/state/ /var/lib/arveld/
COPY docker/arveld.yml /etc/arveld/arveld.yml
WORKDIR /var/lib/arveld
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/arveld"]
CMD ["--config=/etc/arveld/arveld.yml"]
