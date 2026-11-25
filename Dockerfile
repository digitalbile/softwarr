# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

# The web and Go stages run on the build machine's own platform. Go
# cross-compiles for the target, so multi-arch builds skip emulation.
FROM --platform=$BUILDPLATFORM node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
ARG VERSION=dev
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/softwarr ./cmd/softwarr

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache su-exec ca-certificates tzdata
COPY --from=build /out/softwarr /app/softwarr
COPY docker/softwarr-wrapper.sh /usr/local/bin/softwarr
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /usr/local/bin/softwarr /entrypoint.sh
ENV SOFTWARR_DATA_DIR=/config PUID=1000 PGID=1000 UMASK=022
EXPOSE 7979
VOLUME /config
ENTRYPOINT ["/entrypoint.sh"]
