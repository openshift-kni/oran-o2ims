# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.26@sha256:3ca2c34c83e6563fa574b7c7ecda92a5fdae2ec7a5f37a408e5e383947fb1957 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/ubi9/ubi-minimal-pqc:latest@sha256:ef0438e9f078e71a7317c247fdf1d09644441fefe1fe26efbde5f4fa201435fb

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
