<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Contributing Guidelines

## Terms

All contributions to this repository must be submitted under the terms of the
[Apache Public License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Certificate of Origin

By contributing to this project, you agree to the Developer Certificate of
Origin (DCO). This document was created by the Linux Kernel community and is a
simple statement that you, as a contributor, have the legal right to make the
contribution. See the [DCO](DCO) file for details.

## DCO Sign Off

You must sign off your commit to state that you certify the DCO. To certify
your commit for DCO, add a line like the following at the end of your commit
message:

```text
Signed-off-by: John Smith <john@example.com>
```

This can be done with the `--signoff` (or `-s`) option to `git commit`. See the
[Git documentation](https://git-scm.com/docs/git-commit#Documentation/git-commit.txt--s)
for details. All commits must include the DCO sign-off line.

## Development Environment Setup

### Prerequisites

- Go (see `go.mod` for the required version)
- Docker or Podman for building container images
- Make
- Access to an OpenShift hub cluster with ACM for deployment and testing
  (see [Prerequisites](docs/user-guide/prereqs.md) for minimum versions)

### Getting Started

1. Clone the repository:

   ```bash
   git clone https://github.com/openshift-kni/oran-o2ims.git
   cd oran-o2ims
   ```

2. Build and deploy to your hub cluster:

   ```bash
   make IMAGE_TAG_BASE=quay.io/<your-repo>/oran-o2ims VERSION=latest \
     docker-build docker-push install deploy
   ```

   To undeploy:

   ```bash
   make undeploy uninstall
   ```

   To rebuild and restart pods after code changes:

   ```bash
   make IMAGE_TAG_BASE=quay.io/<your-repo>/oran-o2ims VERSION=latest \
     docker-build docker-push && \
     oc delete pods -n oran-o2ims --field-selector=status.phase==Running
   ```

   For deployment using bundles or catalogs, see
   [Environment Setup](docs/user-guide/environment-setup.md).

## Development Workflow

### Running CI Checks Locally

Before submitting a pull request, run the CI validation suite locally:

```bash
make ci-job
```

This runs formatting, vetting, linting, unit tests, e2e tests, and bundle
validation — the same checks that CI runs on each pull request. Running this
locally allows you to find and fix issues before the CI runs.

Additional checks to run depending on the type of change:

- **Documentation changes**: Run `make markdownlint` to lint markdown files.
- **Bundle metadata changes** (e.g., adding new CRDs, updating fields):
  Run `make scorecard-test` to validate the operator bundle.

## Testing Requirements

All code changes must include appropriate unit tests:

1. Write unit tests that cover new code paths.
2. Ensure existing tests still pass.
3. `make ci-job` runs both unit tests (`make test`) and e2e tests
   (`make test-e2e`).

## API and CRD Changes

When modifying API types in `api/`:

1. Ensure changes are backward compatible when possible (use optional fields
   for new additions).
2. After API changes, regenerate manifests and code:

   ```bash
   make generate
   make manifests
   make bundle
   ```

3. Update sample CRs in `config/samples/` and `docs/samples/` to reflect
   changes.

## Documentation

When adding new features or changing behavior, update the relevant
documentation under `docs/user-guide/`. Run `make markdownlint` to validate
formatting. See the [README](README.md#user-guide) for the documentation
structure.

## Pull Request Guidelines

- All pull requests must be opened against the `main` branch.
- Ensure all commits are signed off with DCO (`git commit -s`).
- Run `make ci-job` locally and ensure all checks pass before submitting.
- If updating documentation, also run `make markdownlint`.
- If updating bundle metadata (e.g., adding new CRDs, updating fields), also
  run `make scorecard-test`.

### AI-Generated Code Disclosure

If you used AI tools to generate or assist with your code changes, disclose
this in your commit messages using a trailer such as `Assisted-By` or
`Co-Authored-By`. For example:

```text
Assisted-By: Claude Code <noreply@anthropic.com>
```

All AI-generated code must be reviewed and understood by the contributor.
Contributors remain fully responsible for ensuring the code meets project
standards.
