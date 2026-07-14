FROM golang:1.25-alpine AS builder

# Install git and certificates (needed if your app fetches external dependencies or makes HTTPS calls)
# RUN apk update && apk add --no-cache git ca-certificates && update-ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build the binary
# RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o myapp .
RUN go build -o myapp .

FROM alpine:3.20
RUN apk update && apk add --no-cache ffmpeg
# Copy SSL certificates from the builder stage so HTTPS requests work
# COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/myapp /myapp

ENTRYPOINT ["/myapp"]
