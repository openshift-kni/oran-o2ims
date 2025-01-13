#!/bin/bash

function usage {
    cat <<EOF >&2
Paramaters:
    --namespace <namespace>
    --package <package name>
    --channel <channel>
    --catalog-image <image ref>
    --install-mode <OwnNamespace | AllNamespaces>
EOF
    exit 1
}

function generateCatalogSource {
    cat <<EOF
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  annotations:
    target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
  name: ${PACKAGE}
  namespace: openshift-marketplace
spec:
  displayName: ${PACKAGE}
  image: ${CATALOG_IMG}
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 1h
EOF
}

function generateNamespace {
    cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  annotations:
    workload.openshift.io/allowed: management
EOF
}

function generateOperatorGroup {
    if [ "${INSTALL_MODE}" = "OwnNamespace" ]; then
    cat <<EOF
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${PACKAGE}
  namespace: ${NAMESPACE}
spec:
  targetNamespaces:
  - ${NAMESPACE}
EOF
    else
    cat <<EOF
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${PACKAGE}
  namespace: ${NAMESPACE}
EOF
    fi
}

function generateSubscription {
    cat <<EOF
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ${PACKAGE}
  namespace: ${NAMESPACE}
spec:
  channel: ${CHANNEL}
  name: ${PACKAGE}
  source: ${PACKAGE}
  sourceNamespace: openshift-marketplace
EOF
}

#
# Command-line processing
#
declare PACKAGE=
declare NAMESPACE=
declare CHANNEL=
declare CATALOG_IMG=
declare INSTALL_MODE=

longopts=(
    "help"
    "namespace:"
    "package:"
    "catalog-image:"
    "channel:"
    "install-mode:"
)

longopts_str=$(IFS=,; echo "${longopts[*]}")

if ! OPTS=$(getopt -o "ho:" --long "${longopts_str}" --name "$0" -- "$@"); then
    usage
    exit 1
fi

eval set -- "${OPTS}"

while :; do
    case "$1" in
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --package)
            PACKAGE="$2"
            shift 2
            ;;
        --catalog-image)
            CATALOG_IMG="$2"
            shift 2
            ;;
        --channel)
            CHANNEL="$2"
            shift 2
            ;;
        --install-mode)
            INSTALL_MODE="$2"
            shift 2
            ;;
        --)
            shift
            break
            ;;
        *)
            usage
            ;;
    esac
done

if [ -z "${NAMESPACE}" ] || [ -z "${PACKAGE}" ] || [ -z "${CATALOG_IMG}" ] || [ -z "${CHANNEL}" ] || [ -z "${INSTALL_MODE}" ]; then
    usage
fi

generateCatalogSource
generateNamespace
generateOperatorGroup
generateSubscription
