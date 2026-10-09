FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=frontend /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bolt-dock ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 bolt \
    && adduser -S -D -H -u 10001 -G bolt bolt \
    && mkdir -p /data && chown bolt:bolt /data
WORKDIR /app
COPY --from=backend --chown=bolt:bolt /out/bolt-dock /app/bolt-dock
ENV PORT=18765 DATA_DIR=/data
EXPOSE 18765
VOLUME ["/data"]
USER 10001:10001
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:18765/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/bolt-dock"]
