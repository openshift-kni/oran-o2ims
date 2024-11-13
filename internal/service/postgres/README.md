# Postgres for O-CLOUD-DB

# IMPORTANT NOTE: Only an operator will finally manage PG service. Files here are simply here to unblock any work with api implementation and test. Please delete these static k8s files once we have this integrated. Update anything else as needed (e.g makefile).

This directory contains everything needed to deploy an Postgres for o-cloud. 

- Container source: https://github.com/sclorg/postgresql-container
- Catalog: https://catalog.redhat.com/software/containers/rhel9/postgresql-16/657b03866783e1b1fb87e142
  - Here you may also find additional ENV variables that maybe useful for production overlay.

## Deploy Dev Postgres service

```shell
make run-postgres
```
- This deploys a dev ephemeral postgres instance using [kustomize](./k8s/base/deployment.yaml)
- [Tunes](./k8s/base/postgresql-cfg) and [provisions](./k8s/base/postgresql-start) DB
- Adds dev [labels](./k8s/overlays/dev/kustomization.yaml)

## Note for when setting up the prod env
- Add PVC to deployment 
- Generate password for each service 
- Check the official RH pg doc for any additional config values that can be used during deployment
- Investigate ODF
