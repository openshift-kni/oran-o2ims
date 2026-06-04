# PR Review Feedback

## Review Summary

@telco-sdlc-bot: The following test **failed**, say `/retest` to rerun all failed tests or `/retest-required` to rerun all mandatory failed tests:

Test name | Commit | Details | Required | Rerun command
--- | --- | --- | --- | ---
ci/prow/ci-job | 04e811c8b0c5380d681b3e36025c99f145c5f781 | [link](https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/openshift-kni_oran-o2ims/2749/pull-ci-openshift-kni-oran-o2ims-main-ci-job/2062662465719635968) | true | `/test ci-job`

[Full PR test history](https://prow.ci.openshift.org/pr-history?org=openshift-kni&repo=oran-o2ims&pr=2749). [Your PR dashboard](https://prow.ci.openshift.org/pr?query=is:pr+state:open+author:telco-sdlc-bot).

<details>

Instructions for interacting with me using PR comments are available [here](https://git.k8s.io/community/contributors/guide/pull-requests.md).  If you have questions or suggestions related to my behavior, please file an issue against the [kubernetes-sigs/prow](https://github.com/kubernetes-sigs/prow/issues/new?title=Prow%20issue:) repository. I understand the commands that are listed [here](https://go.k8s.io/bot-commands).
</details>
<!-- test report -->


## Inline Comments

### `internal/cmd/operator/start_controller_manager.go` (line 379)

Could we change this to "Hello, Friends!" instead?

