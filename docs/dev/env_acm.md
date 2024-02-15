# ACM configuration

### [Install ACM Operator](https://github.com/stolostron/multiclusterhub-operator)
From web console:
* Operators > OperatorHub > Install `Advanced Cluster Management for Kubernetes` (if not already installed)
* Create a `MultiClusterHub` instance (when prompted)
* Operators > Installed Operators > ACM > MultiClusterHub > Wait for Status `Running`

## [Search Query API](https://github.com/stolostron/search-v2-operator/wiki/Search-Query-API)

### Create the search-api route
```bash
oc create route passthrough search-api --service=search-search-api -n open-cluster-management
```

### Enable the search collector
For every managed cluster, create a `KlusterletAddonConfig` with enabled `searchCollector`:
```yaml
apiVersion: agent.open-cluster-management.io/v1
kind: KlusterletAddonConfig
metadata:
  name: mgmt-spoke1
  namespace: mgmt-spoke1
spec:
  searchCollector:
    enabled: true
  applicationManager:
    enabled: true
  certPolicyController:
    enabled: true
  iamPolicyController:
    enabled: true
  policyController:
    enabled: true
```

### Create a token for accessing the API
```bash
oc create token oauth-apiserver-sa -n openshift-oauth-apiserver --duration=8760h
```

### Query the API
POST https://search-api-open-cluster-management.apps.oran-hub01.rdu-infra-edge.corp/searchapi/graphql

```
query mySearch($input: [SearchInput]) {
    searchResult: search(input: $input) {
    		items,      
        }
}

# GraphQL vars
{"input":[
    {
        "filters":[
            {"property":"kind","values":["Cluster"]}]
    }
]}
```

## [Multicluster Global Hub](https://github.com/stolostron/multicluster-global-hub)

### Install
* OperatorHub > Multicluster Global Hub Operator
* Create a MulticlusterGlobalHub CR (e.g. using the web console)

### Config
Edit the CSV:
```bash
oc -n multicluster-global-hub edit csv multicluster-global-hub-operator.v1.1.0-dev
```

Add the following under 'containers.args':
```bash
- --global-resource-enabled
```
Note: in order to test the functionality of the global hub, ACM should be installed on the spoke clusters.

## [Observability](https://github.com/stolostron/multicluster-observability-operator)

### Prerequisites
#### Prepare storage
Clone assisted-service:
```bash
git clone https://github.com/openshift/assisted-service
```

Install and configure LSO:
```bash
cd assisted-service/deploy/operator/
export DISKS=$(echo sd{b..f})
./libvirt_disks.sh create
./setup_lso.sh install_lso
./setup_lso.sh create_local_volume
oc patch storageclass assisted-service -p '{"metadata": {"annotations": {"storageclass.kubernetes.io/is-default-class": "true"}}}'
```

Run minio (for S3 compatible object storage):
```bash
podman run -d -p 9000:9000 -p 9001:9001 -v ~/minio/data:/data
-e "MINIO_ROOT_USER=accessKey1" -e "MINIO_ROOT_PASSWORD=verySecretKey1"
quay.io/minio/minio server /data --console-address ":9001"
```

### Create namespace
```bash
oc create namespace open-cluster-management-observability
```

### Create operator pull secret
```
DOCKER_CONFIG_JSON=`oc extract secret/pull-secret -n openshift-config --to=-`
oc create secret generic multiclusterhub-operator-pull-secret \
    -n open-cluster-management-observability \
    --from-literal=.dockerconfigjson="$DOCKER_CONFIG_JSON" \
    --type=kubernetes.io/dockerconfigjson
```

### Apply Thanos Secret
oc apply -f thanos-secret.yaml
```bash
apiVersion: v1
kind: Secret
metadata:
  name: thanos-object-storage
  namespace: open-cluster-management-observability
type: Opaque
stringData:
  thanos.yaml: |
    type: s3
    config:
      bucket: test
      endpoint: <host_ip>:9000
      insecure: true
      access_key: accessKey1
      secret_key: verySecretKey1
```
Note: change <host_ip>

### Apply MultiClusterObservability
oc apply -f mco.yaml
```bash
apiVersion: observability.open-cluster-management.io/v1beta2
kind: MultiClusterObservability
metadata:
  name: "observability"
spec:
  observabilityAddonSpec: {}
  storageConfig:
    metricObjectStorage:
      name: thanos-object-storage
      key: thanos.yaml
    compactStorageSize: 50Gi
    receiveStorageSize: 50Gi
  advanced:
    query:
      resources:
        limits:
          cpu: 1
          memory: 1Gi
      replicas: 1
    receive:
      resources:
        limits:
          cpu: 1
          memory: 1Gi
      replicas: 1
    rule:
      resources:
        limits:
          cpu: 1
          memory: 1Gi
      replicas: 1
    store:
      resources:
        limits:
          cpu: 1
          memory: 1Gi
      replicas: 1
    storeMemcached:
      resources:
        limits:
          cpu: 1
          memory: 1Gi
      replicas: 1
    queryFrontendMemcached:
      resources:
        limits:
          cpu: 1
          memory: 1Gi
      replicas: 1
    alertmanager:
      replicas: 1
```

### Create a token for accessing the API
```bash
export TOKEN=$(oc create token oauth-apiserver-sa -n openshift-oauth-apiserver --duration=8760h)
```

### Access AlertManager API
https://alertmanager-open-cluster-management-observability.apps.ostest.test.metalkube.org/api/v2/alerts
```bash
curl -k -H "Authorization: Bearer $TOKEN" https://alertmanager-open-cluster-management-observability.apps.ostest.test.metalkube.org/api/v2/alerts | jq
```
