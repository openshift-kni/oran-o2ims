# Build stage
FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.25-rhel9@sha256:15ccb52a3f8f458b79f20535925c5069b381658c30879605c276d804b431e5ea AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:baafce2d34783a28b4a472779f304e97e00f9d35c99553bc53b068b14344d9a9

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
