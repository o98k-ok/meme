# syntax=docker/dockerfile:1.7

# ---- builder ---------------------------------------------------------------
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Mainland-friendly module proxy — the default proxy.golang.org times out
# from CN-region builders. goproxy.cn is a public mirror; `,direct` keeps
# the door open for private modules later. Remove these two lines if your
# builder reaches proxy.golang.org directly.
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn

# Cache module deps first so source-only edits don't re-pull dependencies.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/meme-server ./cmd/server

# ---- runtime ---------------------------------------------------------------
FROM alpine:3.20

# Outbound HTTPS to qudoutu/sogou/doutula/doutub/douyin needs CA bundle.
RUN apk add --no-cache ca-certificates tzdata curl \
    && update-ca-certificates

WORKDIR /app
COPY --from=builder /out/meme-server /usr/local/bin/meme-server

EXPOSE 18080

# Default listen-on-all-interfaces so the host network mapping just works.
# MEME_PUBLIC_URL must point at how clients see this server (typically a
# private-network IP + the bound port). Set it via compose env so the
# qudoutu / doutub image URLs that get rewritten to /img round-trip
# correctly.
ENV MEME_HTTP_LISTEN=":18080"

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -fsS http://127.0.0.1:18080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/meme-server"]
