FROM --platform=$BUILDPLATFORM golang:1.27.1@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=4 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -p=4 -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" -tags=grpcnotrace -o /out/arveld-agent ./cmd/arveld-agent
RUN mkdir -p /out/state

# Export this target to obtain the exact executable used by the runtime image.
FROM scratch AS binary
COPY --from=build /out/arveld-agent /arveld-agent
COPY LICENSE NOTICE /

FROM scratch AS runtime
ARG VERSION=dev
ARG REVISION
LABEL org.opencontainers.image.source="https://github.com/RSTCK-Innovation/arveld" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=binary /arveld-agent /arveld-agent
COPY --from=binary /LICENSE /NOTICE /usr/share/licenses/arveld-agent/
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/state/ /var/lib/arveld-agent/
USER 10001:10001
ENV ARVELD_HOST_ROOT=/hostfs
ENTRYPOINT ["/arveld-agent"]
CMD ["--storage-directory=/var/lib/arveld-agent"]
