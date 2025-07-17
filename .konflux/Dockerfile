# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.23@sha256:96cfceb50f5323efa1aa8569d4420cdbf1bb391225d5171ef72a0d0ecf028467 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:9c8ba349820516dd18033944073d68b7623718ba9796ffe3b5bcf04bd9fa637d

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
