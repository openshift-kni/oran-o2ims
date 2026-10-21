<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# External CRD Dependencies

The operator watches CRDs from external operators that are not part of
our own operator bundle. The required operators are listed in the
[user-facing prerequisites](../user-guide/prereqs.md#required-operators-and-add-ons-on-the-hub).

When adding an `Owns` or `Watches` on a CRD from one of these external
operators, update the following:

1. The [CI prereq stub script](https://github.com/openshift/release/tree/master/ci-operator/step-registry/oran-o2ims/install-prereq-crds)
   in the openshift/release repo — the TLS scan CI environment installs
   only ACM and our bundle. A missing CRD causes the controller-manager's
   informer cache sync to time out, crash-looping the manager.
2. Add the CRD to `ExternalCrdData` in
   [test/utils/vars.go](../../test/utils/vars.go) — the e2e suite
   starts the full controller manager, which registers all watches at
   startup. Ensure the API module is declared in `go.mod`.
