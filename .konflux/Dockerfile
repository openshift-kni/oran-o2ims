# Build stage
FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.25-rhel9@sha256:9858b4775f7a6447f72d55d900c71f9ea74ce857caac4c1ac55348c4c1c56a5e AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:6f3a78179e0b30b930a186e7f68a4af52e51bb82406c9653be10bddd624e40f5

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
