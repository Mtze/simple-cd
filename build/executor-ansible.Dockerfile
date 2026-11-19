# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f build/executor-ansible.Dockerfile .

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/simple-cd-executor ./cmd/simple-cd-executor

FROM alpine:3.24.4
ARG ANSIBLE_CORE_VERSION=2.22.0
RUN apk add --no-cache ca-certificates git openssh-client python3 \
    && python3 -m venv /opt/ansible \
    && /opt/ansible/bin/pip install --no-cache-dir "ansible-core==${ANSIBLE_CORE_VERSION}" \
    && adduser -D -H -u 10001 -h /tmp executor \
    && mkdir -m 0700 /secrets && chown 10001:10001 /secrets
COPY --from=build /out/simple-cd-executor /usr/local/bin/simple-cd-executor
ENV HOME=/tmp PATH=/opt/ansible/bin:$PATH
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/simple-cd-executor"]
