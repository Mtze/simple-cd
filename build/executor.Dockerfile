# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f build/executor.Dockerfile .

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/simple-cd-executor ./cmd/simple-cd-executor

FROM alpine:3.24.4 AS tools
ARG TARGETARCH
# Bump versions together with their checksums from
# https://get.helm.sh/helm-<v>-linux-<arch>.tar.gz.sha256sum and
# https://dl.k8s.io/release/<v>/bin/linux/<arch>/kubectl.sha256
ARG HELM_VERSION=v4.3.2
ARG HELM_SHA256_amd64=d1c0278d46beab79582d7e50f5b3dc206944ba8e577cde618513fd07daa53983
ARG HELM_SHA256_arm64=df31cb61680ef728f82c770e5280dd565617bee6b039d37e85c677288be4bbb0
ARG KUBECTL_VERSION=v1.37.2
ARG KUBECTL_SHA256_amd64=c04487523e7aa7d1493f7808174c899db93d7a9cf57531420cd76b9bd415f8f4
ARG KUBECTL_SHA256_arm64=fcc16f63186659d9722014b7e02d5fdd2d4e669cb8c0ab66365d7d3b8ba03509
RUN apk add --no-cache curl
WORKDIR /dl
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) helm_sha=$HELM_SHA256_amd64; kubectl_sha=$KUBECTL_SHA256_amd64 ;; \
      arm64) helm_sha=$HELM_SHA256_arm64; kubectl_sha=$KUBECTL_SHA256_arm64 ;; \
      *) echo "unsupported TARGETARCH $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo helm.tar.gz "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    echo "$helm_sha  helm.tar.gz" | sha256sum -c -; \
    tar -xzf helm.tar.gz "linux-${TARGETARCH}/helm"; \
    install -m 0755 "linux-${TARGETARCH}/helm" /usr/local/bin/helm; \
    curl -fsSLo kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl"; \
    echo "$kubectl_sha  kubectl" | sha256sum -c -; \
    install -m 0755 kubectl /usr/local/bin/kubectl

FROM alpine:3.24.4
RUN apk add --no-cache ca-certificates openssh-client \
    && adduser -D -H -u 10001 -h /tmp executor \
    && mkdir -m 0700 /secrets && chown 10001:10001 /secrets
COPY --from=tools /usr/local/bin/helm /usr/local/bin/kubectl /usr/local/bin/
COPY --from=build /out/simple-cd-executor /usr/local/bin/simple-cd-executor
ENV HOME=/tmp
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/simple-cd-executor"]
