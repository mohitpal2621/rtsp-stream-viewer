# syntax=docker/dockerfile:1

# One image for the whole demo: the Go backend serves the built React app and
# the API, and MediaMTX runs alongside it with looping test streams so there
# is always something to watch. Set DEMO_SOURCES=off to run without them.

FROM node:24-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.23-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.24 AS base
RUN apk add --no-cache ffmpeg tini

# Rendered at build time so the running container only copies video, which
# costs almost no CPU on a small instance.
FROM base AS clips
COPY demo/make-clips.sh /make-clips.sh
RUN sh /make-clips.sh /media

FROM bluenviron/mediamtx:1.21.1 AS mediamtx

FROM base
COPY --from=mediamtx /mediamtx /usr/local/bin/mediamtx
COPY --from=clips /media /media
COPY --from=backend /out/server /usr/local/bin/server
COPY --from=frontend /src/dist /srv/www
COPY demo/mediamtx.yml /etc/mediamtx.yml
COPY demo/entrypoint.sh /usr/local/bin/entrypoint
RUN chmod 755 /usr/local/bin/entrypoint

ENV PORT=8080 \
    STATIC_DIR=/srv/www \
    DEMO_STREAMS="Test pattern=rtsp://localhost:8554/testsrc,Mandelbrot=rtsp://localhost:8554/mandelbrot,Game of Life=rtsp://localhost:8554/life,Gradients=rtsp://localhost:8554/gradients"

RUN adduser -D -H -u 10001 viewer
USER viewer
EXPOSE 8080 8554
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/entrypoint"]
