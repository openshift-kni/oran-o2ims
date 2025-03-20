#!/bin/bash

ssh-keygen -q -t ed25519 -C "github-repo" -f /tmp/github-ed25519 <<< $'\ny' >/dev/null 2>&1
oc create -n openshift-gitops secret generic ocloud-manager-repo --from-file=sshPrivateKey=/tmp/github-ed25519 --from-literal=type=git --from-literal=url=git@github.com:openshift-kni/oran-o2ims.git --from-literal=insecure=true
oc label -n openshift-gitops secret ocloud-manager-repo argocd.argoproj.io/secret-type=repository

echo "sshPrivateKey has been added into ArgoCD repository."
echo
echo "Then go to https://github.com/openshift-kni/oran-o2ims, click Settings -> Security -> Deploy keys"
echo "Add a new deploy key"
echo "Title: argocd"
echo "Key: $(cat /tmp/github-ed25519.pub)"
echo
