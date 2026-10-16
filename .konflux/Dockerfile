# Build stage
FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.25-rhel9@sha256:c7aa23b17189f8e76dec4bc7eada3d52a65a6a54b259c16fb13c15f9d23f793a AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:c4c285d42941ebb2bd4da878479008e1e4536bb599aad1dbe568f89de9053661

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
