# Build stage
FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.26-rhel9@sha256:036022b8a9057a8cf5b09ba9fd8519ca86ffbebde5501f366d9156f8cec01b1d AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/ubi9/ubi-minimal-pqc:latest@sha256:e8673bd338b3f093501a9a9088f043d8959df120edd9947583e2d9bb3aacc3a8

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
