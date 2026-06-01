FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /bin/ipdb-manager .

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/ipdb-manager /usr/local/bin/ipdb-manager

EXPOSE 9090
ENTRYPOINT ["ipdb-manager"]
CMD ["-config", "/etc/ipdb-manager/config.yaml"]
