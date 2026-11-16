FROM golang:1.23-alpine3.20 AS builder

WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -S -u 10001 app
WORKDIR /app
COPY --from=builder /gateway /app/gateway

USER app
EXPOSE 8080
CMD ["/app/gateway"]
