FROM node:22-alpine AS web-builder
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /internal/server/static/dist ./internal/server/static/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o icloud-prime .

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/icloud-prime .
EXPOSE 8081
ENTRYPOINT ["/app/icloud-prime"]
CMD ["-addr", ":8081"]
