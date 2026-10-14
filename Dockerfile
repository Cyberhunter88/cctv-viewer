# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:24.10.0-bookworm-slim AS frontend
WORKDIR /src
COPY package.json package-lock.json tsconfig.json ./
RUN npm ci
COPY web/ web/
RUN npm run typecheck && npm test && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/embed.go
COPY --from=frontend /src/web/dist/ web/dist/
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /cctv ./cmd/cctv

FROM debian:bookworm-slim
RUN apt-get update && apt-get install --no-install-recommends -y ffmpeg ca-certificates tzdata tini && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 10001 cctv && useradd -u 10001 -g cctv -M -d /data -s /usr/sbin/nologin cctv \
    && mkdir -p /data /recordings && chown cctv:cctv /data
COPY --from=backend /cctv /usr/local/bin/cctv
USER 10001:10001
EXPOSE 8080
ENV CTV_DATA_DIR=/data CTV_RECORDINGS_DIR=/recordings
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/cctv","healthcheck"]
ENTRYPOINT ["/usr/bin/tini","--","/usr/local/bin/cctv"]
