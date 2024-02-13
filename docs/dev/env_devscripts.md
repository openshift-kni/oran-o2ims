# Setup development environment

Follow these instructions to deploy an ACM hub cluster with additional managed clusters.
The deployment is done by using [dev-scripts](https://github.com/openshift-metal3/dev-scripts) to install installs a 3-nodes cluster ACM hub and 2 spoke clusters.
Note: this flow is useful if a DNS configuration is mandatory. E.g. for using the Observability service which requires the Hub cluster to be reachable from spoke clusters using the hostname.

## Deployment

### Requirements
Ensure libvirt is available on the host:
```bash
sudo yum -y install libvirt libvirt-daemon-driver-qemu qemu-kvm
sudo usermod -aG qemu,libvirt $(id -un)
sudo newgrp libvirt
sudo systemctl enable --now libvirtd
```

### Clone dev-scripts
```bash
git clone https://github.com/openshift-metal3/dev-scripts
```

### Configuration

#### Create a config file
```bash
cp config_example.sh config_$USER.sh
```

#### Set CI_TOKEN
Go to https://console-openshift-console.apps.ci.l2s4.p1.openshiftapps.com/, click on your name in the top
right, copy the login command, extract the token from the command and use it to set `CI_TOKEN` in `config_$USER.sh`.

#### Get pull secret
Save the secret obtained from [cloud.openshift.com](https://cloud.redhat.com/openshift/install/pull-secret) to `pull_secret.json`.

#### Set config vars
Add the following the `config_$USER.sh` file:
```bash
# Control plane
export NUM_MASTERS=3
export NUM_WORKERS=0
export MASTER_MEMORY=65536
export MASTER_DISK=120
export MASTER_VCPU=16
# Extra nodes
export NUM_EXTRA_WORKERS=2
export EXTRA_WORKER_VCPU=8
export EXTRA_WORKER_MEMORY=16384
export EXTRA_WORKER_DISK=120
# General
export OPENSHIFT_RELEASE_STREAM=4.14
export IP_STACK=v4
export PROVISIONING_NETWORK_PROFILE=Disabled
export REDFISH_EMULATOR_IGNORE_BOOT_DEVICE=True
```

### Installation

#### Run
```bash
cd dev-scripts
make
```

#### Clean
```bash
make clean
```

## Access the hub cluster

### kubeconfig
```bash
export KUBECONFIG=/home/git/dev-scripts/ocp/ostest/auth/kubeconfig
```

### Web Console

#### Configure the local /etc/hosts
```bash
<host_ip> console-openshift-console.apps.ostest.test.metalkube.org grafana-open-cluster-management-observability.apps.ostest.test.metalkube.org observatorium-api-open-cluster-management-observability.apps.ostest.test.metalkube.org alertmanager-open-cluster-management-observability.apps.ostest.test.metalkube.org
```

#### Install and configure xinetd

##### Install
```bash
sudo dnf install xinetd
```

##### Find API VIP
```bash
cat /etc/NetworkManager/dnsmasq.d/openshift-ostest.conf
```
E.g. address=/api.ostest.test.metalkube.org/11.0.0.5

##### Add config file
*/etc/xinetd.d/openshift*
```bash
service openshift-ingress-ssl
{
    flags           = IPv4
    bind            = <host_ip>
    disable         = no
    type            = UNLISTED
    socket_type     = stream
    protocol        = tcp
    user            = root
    wait            = no
    redirect        = 10.0.0.5 443
    port            = 443
    only_from       = 0.0.0.0/0
    per_source      = UNLIMITED
}
```

##### Restart xinetd
```bash
sudo systemctl restart xinetd
```

#### Access the Web Console
Navigate to: https://console-openshift-console.apps.ostest.test.metalkube.org
* User: kubeadmin
* Password: `cat /home/git/dev-scripts/ocp/ostest/auth/kubeadmin-password`

## Operators configuration

### Install ACM Operator

From web console:
* Operators > OperatorHub > Install `Advanced Cluster Management for Kubernetes`
* Create a `MultiClusterHub` instance (when prompted)
* Operators > Installed Operators > ACM > MultiClusterHub > Wait for Status `Running`

### Enable Observability service

#### Prerequisites

##### Prepare storage

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

#### Create namespace
```bash
oc create namespace open-cluster-management-observability
```

#### Create operator pull secret
```
DOCKER_CONFIG_JSON=`oc extract secret/pull-secret -n openshift-config --to=-`
oc create secret generic multiclusterhub-operator-pull-secret \
    -n open-cluster-management-observability \
    --from-literal=.dockerconfigjson="$DOCKER_CONFIG_JSON" \
    --type=kubernetes.io/dockerconfigjson
```

#### Apply Thanos Secret
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

#### Apply MultiClusterObservability
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

#### Create a token for accessing the API
```bash
export TOKEN=$(oc create token oauth-apiserver-sa -n openshift-oauth-apiserver --duration=8760h)
```

#### Access AlertManager API
https://alertmanager-open-cluster-management-observability.apps.ostest.test.metalkube.org/api/v2/alerts
```bash
curl -k -H "Authorization: Bearer $TOKEN" https://alertmanager-open-cluster-management-observability.apps.ostest.test.metalkube.org/api/v2/alerts | jq
```

## Deploy spoke clusters

### Enable assisted-service
oc apply -f asc.yaml
```bash
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
 name: agent
spec:
  databaseStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 8Gi
  filesystemStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 8Gi
  imageStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
```

### Create a deploy script
/assisted-service/deploy/operator/ztp/deploy.sh
```bash
# Spoke
export SPOKE_NAME="spoke$1"
export SPOKE_NAMESPACE=openshift-machine-api
export ASSISTED_CLUSTER_NAME=$SPOKE_NAME
export ASSISTED_CLUSTER_DEPLOYMENT_NAME=$SPOKE_NAME
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME=$SPOKE_NAME
export ASSISTED_INFRAENV_NAME=$SPOKE_NAME
# OCP version
export ASSISTED_OPENSHIFT_VERSION=openshift-v4.14
export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE=quay.io/openshift-release-dev/ocp-release:4.14.12-x86_64
# Pull secret
export ASSISTED_PULLSECRET_JSON=/home/git/dev-scripts/pull_secret.json
# Extra hosts
cat /home/git/dev-scripts/ocp/ostest/extra_baremetalhosts.json | jq "[nth($1)]" > /home/git/dev-scripts/ocp/ostest/bmh.json
export EXTRA_BAREMETALHOSTS_FILE=/home/git/dev-scripts/ocp/ostest/bmh.json

./deploy_spoke_cluster.sh
```

### Patch BMO Provisioning
```bash
oc patch provisioning provisioning-configuration --type='merge' -p '{"spec":{"watchAllNamespaces":true}}'
```

### Run deploy script
```bash
chmod +x deploy.sh
# Deploy spoke0
./deploy.sh 0
# Deploy spoke1
./deploy.sh 1
```
Note: if an Agent is not discovered for a while, ssh to the machine and start agent.service.
```bash
export IP=$(virsh net-dhcp-leases ostestbm | grep extraworker-0 | awk '{print $5}' | head -c -4)
ssh core@$IP
sudo systemctl start agent
```

### Patch BMHs (if stuck in 'provisioning' state)
```bash
oc -n spoke0 patch bmh ostest-extraworker-0 --type='merge' -p '{"spec":{"online":false}}'
oc -n spoke1 patch bmh ostest-extraworker-1 --type='merge' -p '{"spec":{"online":false}}'
```

### Import a spoke cluster
Navigate to web console:
* All Clusters > Infrastructure > Clusters > Cluster list > spoke0 > Import cluster

### Access a spoke cluster
```bash
# Update /etc/hosts
export SPOKE_APIVIP=$(oc -n spoke0 get aci spoke0 -o json | jq -r '.status.apiVIP')
echo "$SPOKE_APIVIP api.spoke0.redhat.com" >> /etc/hosts
# Fetch kubeconfig
oc extract -n spoke0 secret/spoke0-admin-kubeconfig --to=- > kubeconfig.spoke0
```
