# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.25@sha256:c5d1bbc9c6ba9403017a30c2c71e9e28c9f43ffed2e60573a6de20e8ac753689 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:2e8edce823a48e51858f1fad3ff4cbf6875ce8a3f86b9eecf298bc2050c8652a

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
