# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f build/server.Dockerfile .

FROM --platform=$BUILDPLATFORM node:26.13.1-alpine3.24 AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/simple-cd ./cmd/simple-cd

# Static binary, no shell, runs as nonroot (65532).
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/simple-cd /simple-cd
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/simple-cd"]
