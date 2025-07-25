# Build the manager binary
FROM registry.hub.docker.com/library/golang:1.24 AS dlvbuilder
RUN go install github.com/go-delve/delve/cmd/dlv@latest

FROM dlvbuilder AS builder
ARG GOBUILD_GCFLAGS=""

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Bring in the go dependencies before anything else so we can take
# advantage of caching these layers in future builds.
COPY vendor/ vendor/

# Copy the go modules manifests
COPY go.* .

# Copy the required source directories
COPY main.go .
COPY api api
COPY internal internal
COPY hwmgr-plugins hwmgr-plugins

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -gcflags "${GOBUILD_GCFLAGS}" -mod=vendor -a

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot AS production
WORKDIR /
COPY --from=builder /workspace/oran-o2ims /usr/bin
USER 65532:65532

ENTRYPOINT ["/usr/bin/oran-o2ims"]

FROM registry.access.redhat.com/ubi9/ubi:9.6-1749542372 AS debug

COPY --from=dlvbuilder /go/bin/dlv /usr/bin
COPY --from=builder /workspace/oran-o2ims /usr/bin
USER 65532:65532

ENTRYPOINT ["/usr/bin/oran-o2ims"]
