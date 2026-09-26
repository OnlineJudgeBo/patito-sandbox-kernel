FROM golang:1.24.1-alpine AS builder

WORKDIR /app
COPY . .

RUN go mod download
RUN go build -o /go/bin/isolate-wrapper ./src/cmd/listener/main.go
FROM alpine:latest

WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=builder /go/bin/isolate-wrapper /app/isolate-wrapper

COPY test/ ./test/

ENV GIN_MODE=release
ENV LOG_TO_STDOUT=true

CMD ["/app/isolate-wrapper"]
