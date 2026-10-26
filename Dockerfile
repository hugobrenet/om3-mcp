# The image of the OpenSVC daemon MCP: the binary alone on alpine, run as
# opensvc-mcp, uid 994 and gid 982, the IDs the files and the sockets of the
# service are given, the same on every node.
FROM golang:1.25 AS builder

# VERSION is the version of the binary, from git describe; the git directory
# is not in the build context.
ARG VERSION

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/om3-mcp ./cmd/om3-mcp

FROM alpine:3.22.4
ARG BUILDTIME

RUN addgroup -S -g 982 opensvc-mcp \
    && adduser -S -D -H -u 994 -G opensvc-mcp -s /sbin/nologin opensvc-mcp

COPY --from=builder /out/om3-mcp /usr/bin/om3-mcp

USER 994:982
ENTRYPOINT ["/usr/bin/om3-mcp"]

LABEL \
    org.opencontainers.image.authors="OpenSVC SAS" \
    org.opencontainers.image.created="${BUILDTIME}" \
    org.opencontainers.image.licenses="Apache-2.0" \
    org.opencontainers.image.url="https://github.com/opensvc/om3-mcp"
