# Build stage
FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.26-rhel9@sha256:cdb2b291b842dc836be849d735d47a80c13a910bdd61a95933167bbaff6b8bc3 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/ubi9/ubi-minimal-pqc:latest@sha256:d34666200b64d5a183c867487fd2f48ee8547eb18134e5647099970e26f08af9

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
