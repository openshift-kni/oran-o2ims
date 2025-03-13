#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

function usage {
    cat <<EOF >&2
Paramaters:
    --namespace <namespace>
    --package <package name>
    --crd-search <crd search grep pattern>
EOF
    exit 1
}

function cleanSubscription {
    oc delete subscriptions.operators.coreos.com -n "${NAMESPACE}" "${PACKAGE}"
    oc get csv -n "${NAMESPACE}" | grep "${PACKAGE}" | awk '{print $1}' \
        | xargs --no-run-if-empty oc delete csv -n "${NAMESPACE}"
    oc get crd | grep "${CRD_SEARCH}" | awk '{print $1}' \
        | xargs --no-run-if-empty oc delete crd
    oc delete ns "${NAMESPACE}"
    oc get clusterrole.rbac.authorization.k8s.io | grep "${PACKAGE}" | awk '{print $1}' \
        | xargs --no-run-if-empty oc delete clusterrole.rbac.authorization.k8s.io
    oc get clusterrolebinding.rbac.authorization.k8s.io | grep "${PACKAGE}" | awk '{print $1}' \
        | xargs --no-run-if-empty oc delete clusterrolebinding.rbac.authorization.k8s.io

    oc delete catalogsources.operators.coreos.com -n openshift-marketplace "${PACKAGE}"
}

#
# Command-line processing
#
declare PACKAGE=
declare NAMESPACE=
declare CRD_SEARCH=

longopts=(
    "help"
    "namespace:"
    "package:"
    "crd-search:"
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
        --crd-search)
            CRD_SEARCH="$2"
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

if [ -z "${NAMESPACE}" ] || [ -z "${PACKAGE}" ] || [ -z "${CRD_SEARCH}" ]; then
    usage
fi

cleanSubscription
