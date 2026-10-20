FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 scheduler \
    && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /app scheduler \
    && mkdir -p /app/data && chown 10001:10001 /app/data
WORKDIR /app
COPY --from=build /out/scheduler /app/scheduler
USER 10001:10001
ENV TZ=Asia/Shanghai
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["/app/scheduler", "healthcheck"]
ENTRYPOINT ["/app/scheduler"]
CMD ["-data-dir", "/app/data"]
