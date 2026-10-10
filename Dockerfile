# One toolchain for local builds, CI, and the optional devcontainer.
FROM golang:1.25.3-bookworm AS dev
WORKDIR /workspace
ENV CGO_ENABLED=0

FROM dev AS build
ARG VERSION=dev
COPY . .
RUN go test ./... && go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /aiu ./cmd/aiu

FROM scratch AS binary
COPY --from=build /aiu /aiu
