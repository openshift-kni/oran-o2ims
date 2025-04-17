# Build stage
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:v1.23@sha256:2d5976ded2a3abda6966949c4d545d0cdd88a4d6a15989af38ca5e30e430a619 AS builder

WORKDIR /app
COPY . .

# manager
RUN GOEXPERIMENT=strictfipsruntime CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -tags strictfipsruntime -a -o /bin/oran-o2ims main.go

# Runtime stage
FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:5e1be69fd81a9fe2f58df325ac15fc7e0812d34e6345f5feaf50df38e16d52e3

WORKDIR /

COPY --from=builder /bin/oran-o2ims /usr/bin/oran-o2ims

USER 65532:65532
ENTRYPOINT ["/usr/bin/oran-o2ims"]
