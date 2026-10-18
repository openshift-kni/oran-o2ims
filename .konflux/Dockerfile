# Build stage
FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.26-rhel9@sha256:7e9c3b1b1d35bfc006b18fc5fdff7e0db0ab3136dbba77c2b7f4bd933c0a3235 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/ubi9/ubi-minimal-pqc:latest@sha256:b3a0b012de0c578082e1bc5b01c8bc27461b4eeb571a0b1a2b21a07dbd845a62

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
