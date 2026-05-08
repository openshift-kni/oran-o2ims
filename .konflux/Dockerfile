# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.25@sha256:977bd041377a1367c8b102a460ae8e63f89905f7cf9d8235484ae658c9b47646 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:24650313873554b6ba16c1a1b6b9f9142604f6ab735113e1695faf2dd07fdede

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
