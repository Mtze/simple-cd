# Build from e2e/fakegithub: docker build -t fake-github e2e/fakegithub
FROM golang:1.27.4-alpine3.24 AS build
WORKDIR /src
COPY main.go .
RUN go mod init fakegithub && CGO_ENABLED=0 go build -trimpath -o /fakegithub .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /fakegithub /fakegithub
ENTRYPOINT ["/fakegithub"]
