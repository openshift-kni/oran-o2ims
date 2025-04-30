# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:v1.23@sha256:8c881edc745eeb6ea246f1bac55b09b5ae5dc16f7ed9d0d73b5ffda28e582bab AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:65e57c845402711c5515af0989a2c3c69bf4066396008efd8002be0790fee6c3

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
