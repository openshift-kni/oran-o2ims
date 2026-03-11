# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.24@sha256:3b2a8007ea0d48ce5a15127479cabfa226e55635b92f9012b5752f92c5293e61 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:69f5c9886ecb19b23e88275a5cd904c47dd982dfa370fbbd0c356d7b1047ef68

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
