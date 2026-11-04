# syntax=docker/dockerfile:1

# ---- 1. frontend (runs natively on the build platform; its output is arch-independent) ----
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi
COPY web/ ./
RUN npm run build

# ---- 2. backend (runs on the build platform and cross-compiles via TARGETOS/TARGETARCH, no emulation) ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY skills ./skills
COPY plugins ./plugins
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/genkan ./cmd/genkan
# world-writable so any `user:` works with a fresh named volume
RUN mkdir -p /out/data && chmod 777 /out/data

# ---- 3. runtime ----
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/genkan /genkan
COPY --from=build /out/data /data
ENV GENKAN_DATA=/data \
    GENKAN_ADDR=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/genkan", "healthcheck"]
ENTRYPOINT ["/genkan"]
