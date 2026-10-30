# Build the reference web client and SDK.
FROM node:24-alpine@sha256:6a1c190e17215d844926b0677f4bf87273ff7d5e54db06d3b5b5863ab0b09a3d AS web-builder
WORKDIR /src
COPY package.json package-lock.json ./
COPY web/package.json ./web/package.json
COPY sdk/typescript/package.json ./sdk/typescript/package.json
RUN npm ci
COPY web ./web
COPY sdk ./sdk
RUN npm run -w web build && npm run -w sdk/typescript build

# Build the Go server and the independent reference provider.
FROM golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS go-builder
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/server ./cmd/server
RUN cd examples/capability-provider && \
    CGO_ENABLED=0 go build -trimpath -o /out/capability-provider .

# Minimal non-root runtime for the server.
FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS server
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 -S app \
    && adduser -u 10001 -S -G app -h /app app
WORKDIR /app
COPY --from=go-builder --chown=10001:10001 /out/server /app/server
COPY --from=web-builder --chown=10001:10001 /src/web/dist /app/web
COPY --from=go-builder --chown=10001:10001 /src/tests/fixtures /app/tests/fixtures
COPY --from=go-builder --chown=10001:10001 /src/capabilities.yaml /app/capabilities.yaml
COPY --from=go-builder --chown=10001:10001 /src/LICENSE /app/LICENSE
COPY --from=go-builder --chown=10001:10001 /src/NOTICE /app/NOTICE
COPY --from=go-builder --chown=10001:10001 /src/THIRD_PARTY_NOTICES.md /app/THIRD_PARTY_NOTICES.md
# A named volume mounts here in Compose. The directory itself must exist and be
# owned by the runtime user so an empty volume is writable without relaxing
# read_only.
RUN mkdir -p /app/var/assets && chown -R 10001:10001 /app/var
USER 10001:10001
EXPOSE 8080
ENV WEB_DIST_DIR=/app/web ASSET_DIR=/app/var/assets TMPDIR=/tmp
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/server"]
CMD ["serve"]

# Minimal non-root runtime for the reference capability provider.
FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS provider
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 -S app \
    && adduser -u 10001 -S -G app -h /app app
WORKDIR /app
COPY --from=go-builder --chown=10001:10001 /out/capability-provider /app/capability-provider
USER 10001:10001
EXPOSE 8081
ENV TMPDIR=/tmp LISTEN_ADDR=:8081
ENTRYPOINT ["/app/capability-provider"]
