FROM golang:1.21-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ipdb-manager .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/ipdb-manager /app/ipdb-manager

RUN mkdir -p /etc/ipdb-manager /var/lib/ipdb-manager/ip2region

EXPOSE 9090

ENTRYPOINT ["/app/ipdb-manager"]
CMD ["-config", "/etc/ipdb-manager/config.yaml"]
