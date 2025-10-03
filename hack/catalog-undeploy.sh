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

    oc get csv -n "${NAMESPACE}" --no-headers -o custom-columns=NAME:.metadata.name | grep "${PACKAGE}" \
        | xargs --no-run-if-empty oc delete csv -n "${NAMESPACE}"

    oc get crd --no-headers -o custom-columns=NAME:.metadata.name | grep "${CRD_SEARCH}" \
        | xargs --no-run-if-empty oc delete crd

    oc delete ns "${NAMESPACE}"

    oc get clusterrole.rbac.authorization.k8s.io --no-headers -o custom-columns=NAME:.metadata.name | grep "${NAMESPACE}" \
        | xargs --no-run-if-empty oc delete clusterrole.rbac.authorization.k8s.io
    oc get clusterrolebinding.rbac.authorization.k8s.io --no-headers -o custom-columns=NAME:.metadata.name | grep "${NAMESPACE}" \
        | xargs --no-run-if-empty oc delete clusterrolebinding.rbac.authorization.k8s.io

    oc get rolebindings.rbac.authorization.k8s.io -n kube-system --no-headers -o custom-columns=NAME:.metadata.name | grep "${NAMESPACE}" \
        | xargs --no-run-if-empty oc delete rolebindings.rbac.authorization.k8s.io -n kube-system

    oc delete operators.operators.coreos.com "${PACKAGE}.${NAMESPACE}"

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
