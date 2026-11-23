# syntax=docker/dockerfile:1

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/softwarr ./cmd/softwarr

FROM alpine:3.24
RUN apk add --no-cache su-exec ca-certificates tzdata
COPY --from=build /out/softwarr /app/softwarr
COPY docker/softwarr-wrapper.sh /usr/local/bin/softwarr
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /usr/local/bin/softwarr /entrypoint.sh
ENV SOFTWARR_DATA_DIR=/config PUID=1000 PGID=1000 UMASK=022
EXPOSE 7979
VOLUME /config
ENTRYPOINT ["/entrypoint.sh"]
