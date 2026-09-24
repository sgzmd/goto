# Stage 1: Build binary
FROM golang:alpine AS builder

WORKDIR /src

RUN apk --no-cache add ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-w -s" -trimpath -o /app/goto ./cmd/goto

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

RUN addgroup -S -g 10001 appgroup && \
    adduser -S -u 10001 -G appgroup -h /home/appuser appuser

RUN mkdir -p /data && chown -R appuser:appgroup /data

COPY --from=builder /app/goto /usr/local/bin/goto

USER appuser:appgroup

VOLUME ["/data"]

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/goto"]
