# syntax=docker/dockerfile:1
# Build context is the repository root:  docker build -f deploy/Dockerfile .

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p /src/internal/webdist/dist && npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webdist/dist ./internal/webdist/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pushctl ./cmd/pushctl

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata sqlite \
 && adduser -D -u 10001 app \
 && mkdir -p /data && chown app:app /data
COPY --from=build /out/server /out/pushctl /usr/local/bin/
USER app
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
CMD ["serve"]
