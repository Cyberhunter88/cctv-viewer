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

FROM alpine:3.22
RUN apk add --no-cache ffmpeg ca-certificates tzdata tini \
    && addgroup -g 10001 cctv && adduser -D -u 10001 -G cctv -h /data -s /sbin/nologin cctv \
    && mkdir -p /data /recordings && chown cctv:cctv /data
COPY --from=backend /cctv /usr/local/bin/cctv
USER 10001:10001
EXPOSE 8080
ENV CTV_DATA_DIR=/data CTV_RECORDINGS_DIR=/recordings
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/cctv","healthcheck"]
ENTRYPOINT ["/sbin/tini","--","/usr/local/bin/cctv"]
