FROM golang:1.22-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o dsite ./cmd/dsite

# ── Runtime ──
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/dsite .
COPY templates ./templates
COPY static ./static

RUN mkdir -p /data/uploads

ENV DB_PATH=/data/data.db
ENV UPLOADS_DIR=/data/uploads
ENV PORT=8080

EXPOSE 8080
VOLUME ["/data"]

CMD ["./dsite"]
