# syntax = docker/dockerfile:experimental
FROM hdmap-artifactory-registry-vpc.cn-beijing.cr.aliyuncs.com/hdmap-go-base/golang:1.18.0-alpine AS build-env

ENV GOSUMDB=off \
    GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY="https://artifactory.momenta.works/artifactory/api/go/go"

WORKDIR /workspace
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /bin/esmd ./main.go

FROM hdmap-artifactory-registry-vpc.cn-beijing.cr.aliyuncs.com/docker-hdmap-sre/alpine:3.14.0
RUN ln -s /var/cache/apk /etc/apk/cache
RUN --mount=type=cache,target=/var/cache/apk --mount=type=cache,target=/etc/apk/cache \
    sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories \
    && apk update --no-cache \
    && apk add --no-cache ca-certificates tzdata bash curl xz

COPY --from=build-env /bin/esmd /esmd

EXPOSE 8080
ENTRYPOINT [ "/esmd", "--config", "/config/config.json"]
