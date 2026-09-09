# Build the console SPA.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build the Go binary. Pure Go, so CGO stays off.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=devel
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/yousysadmin/ihttp/pkg.Version=${VERSION}" \
    -o /out/ihttp ./cmd/ihttp

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 1000 ihttp
COPY --from=build /out/ihttp /usr/local/bin/ihttp
USER ihttp
WORKDIR /data
ENV IHTTP_DATA_DIR=/data IHTTP_ADDR=0.0.0.0:8081 IHTTP_PROXY_ADDR=0.0.0.0:8080
EXPOSE 8080 8081
VOLUME /data
ENTRYPOINT ["ihttp"]
CMD ["serve"]
