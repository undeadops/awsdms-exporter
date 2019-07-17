ARG GO_VERSION=1.12

# First stage: build the executable.
FROM golang:${GO_VERSION}-stretch as build_base

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get -y upgrade && \
    apt-get install -y --no-install-recommends git ca-certificates

WORKDIR /src

COPY ./go.mod ./go.sum ./

RUN go mod download


FROM build_base AS build_binaries

COPY . .
# And compile the project
#RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go install -ldflags '-w -extldflags "-static"' .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go install ./cmd/dms-exporter


FROM debian:stretch-slim AS release
# Start Setting up Release Container
ENV DEBIAN_VERSION stretch

# initial install of av daemon
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y && \
    DEBIAN_FRONTEND=noninteractive apt-get install --no-install-recommends -y -qq \
    ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

## Create User to run as
RUN useradd -ms /bin/bash aws

COPY --from=build_binaries /go/bin/dms-exporter /bin/dms-exporter
USER aws

EXPOSE 8080

CMD ["dms-exporter"]
