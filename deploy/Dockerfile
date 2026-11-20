# syntax=docker/dockerfile:1
# Meeting Agent: one image with the Go server, the dashboard and a pinned Chrome.
# Build: docker build -f deploy/Dockerfile -t meeting-agent .
# Run:   docker compose -f deploy/compose.yaml up

# ---- 1. Dashboard (Vite build) ----
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- 2. Go binary (dashboard embedded) ----
FROM golang:1.26-bookworm AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/meeting-agent ./cmd/server

# ---- 3. Runtime: Ubuntu + Chrome for Testing (pinned) + Xvfb ----
FROM ubuntu:24.04
# Chrome for Testing gives exact, versioned Chrome builds. Bump deliberately.
ARG CHROME_VERSION=153.0.8010.12
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl unzip tini xvfb xauth \
      fonts-liberation fonts-noto-core fonts-noto-cjk fonts-noto-color-emoji \
      libnss3 libnspr4 libatk1.0-0t64 libatk-bridge2.0-0t64 libcups2t64 libdrm2 libgbm1 \
      libxkbcommon0 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 libxext6 libx11-xcb1 \
      libasound2t64 libpango-1.0-0 libcairo2 libxshmfence1 libdbus-1-3 libexpat1 libglib2.0-0t64 \
 && curl -fsSL -o /tmp/chrome.zip "https://storage.googleapis.com/chrome-for-testing-public/${CHROME_VERSION}/linux64/chrome-linux64.zip" \
 && unzip -q /tmp/chrome.zip -d /opt && rm /tmp/chrome.zip \
 && ln -s /opt/chrome-linux64/chrome /usr/local/bin/google-chrome \
 && apt-get purge -y curl unzip && apt-get autoremove -y && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 bot && mkdir -p /data && chown bot /data

COPY --from=server /out/meeting-agent /usr/local/bin/meeting-agent
COPY deploy/entrypoint.sh /entrypoint.sh

USER bot
ENV HTTP_ADDR=:8080 \
    DATA_DIR=/data \
    CHROME_PATH=/usr/local/bin/google-chrome
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD ["/usr/local/bin/meeting-agent", "-healthcheck"]
# tini reaps Chrome's child processes (it spawns many) and forwards SIGTERM.
ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
