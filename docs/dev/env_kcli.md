<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Setup development environment

Follow these instructions to deploy an ACM hub cluster with additional managed clusters.
The deployment is done using a [kci](https://github.com/karmab/kcli-openshift4-baremetal) script that installs a 3-nodes cluster ACM hub and 2 spoke clusters.
Note: if the spoke clusters auto-import fails, the clusters should be manually imported to the ACM hub.

## Deployment

### Requirements

Ensure libvirt is available on the host:

```bash
sudo yum -y install libvirt libvirt-daemon-driver-qemu qemu-kvm
sudo usermod -aG qemu,libvirt $(id -un)
sudo newgrp libvirt
sudo systemctl enable --now libvirtd
```

### Install [kcli](https://kcli.readthedocs.io/en/latest/)

```bash
curl https://raw.githubusercontent.com/karmab/kcli/main/install.sh | sudo bash
```

### Clone kcli-openshift4-baremetal

```bash
git clone https://github.com/karmab/kcli-openshift4-baremetal
```

### Create parameters yaml

In the cloned repo, create a kcli_parameters.yaml file and set values as required.
Ensure the following exist in the machine:

* `pullsecret`: path to the pull secret file
* `pool`: libvirt storage pool to store the images

E.g.
*kcli_parameters.yaml*

```yaml
lab: true
pullsecret: /root/pull-secret.json
pool: oran_pool
virtual_ctlplanes: true
version: stable
tag: "4.14"
cluster: oran-hub01
domain: rdu-infra-edge.corp
baremetal_cidr: 192.168.131.0/24
baremetal_net: lab-baremetal
virtual_ctlplanes_memory: 32768
virtual_ctlplanes_numcpus: 8
api_ip: 192.168.131.253
ingress_ip: 192.168.131.252
baremetal_ips:
  - 192.168.131.20
  - 192.168.131.21
  - 192.168.131.22
  - 192.168.131.23
  - 192.168.131.24
baremetal_macs:
  - aa:aa:aa:aa:bb:01
  - aa:aa:aa:aa:bb:02
  - aa:aa:aa:aa:bb:03
  - aa:aa:aa:aa:bb:04
  - aa:aa:aa:aa:bb:05
ztp_spoke_wait: true
ztp_spokes:
- name: mgmt-spoke1
  ctlplanes_number: 1
  workers_number: 0
  virtual_nodes_number: 1
  memory: 65536
- name: mgmt-spoke2
  ctlplanes_number: 1
  workers_number: 0
  virtual_nodes_number: 1
  memory: 65536
disk_size: 90
installer_disk_size: 200
lab_extra_dns:
  - assisted-service-multicluster-engine
  - assisted-service-assisted-installer
  - assisted-image-service-multicluster-engine
notify: true
nfs: true
installer_wait: true
apps:
  - advanced-cluster-management
  - topology-aware-lifecycle-manager
```

### Create the environment

Run the following command to create all the VMs and install ACM:

```bash
kcli create plan -f ./plans/kcli_plan_ztp.yml --paramfile ./kcli_parameters.yaml --force
```

## Access clusters

### Get kubeconfig files

```bash
kcli ssh root@oran-hub01-installer
# Hub cluster kubeconfig
cat /root/ocp/auth/kubeconfig
# Spoke clusters kubeconfig
cat kubeconfig.mgmt-spoke1
cat kubeconfig.mgmt-spoke2
```

### Hub's web console

#### Configure the local /etc/hosts

```bash
<host_ip> api.oran-hub01.rdu-infra-edge.corp console-openshift-console.apps.oran-hub01.rdu-infra-edge.corp oauth-openshift.apps.oran-hub01.rdu-infra-edge.corp assisted-service-multicluster-engine.apps.oran-hub01.rdu-infra-edge.corp search-api-open-cluster-management.apps.oran-hub01.rdu-infra-edge.corp multicluster-global-hub-manager-multicluster-global-hub.apps.oran-hub01.rdu-infra-edge.corp
```

#### Use sshuttle or xinetd

##### sshuttle

```bash
sshuttle -r <user>@<host> 192.168.131.0/24 -v
```

##### xinetd

###### Add config file

/etc/xinetd.d/openshift

```bash
service openshift-api
{
    flags           = IPv4
    bind            = <host_ip>
    disable         = no
    type            = UNLISTED
    socket_type     = stream
    protocol        = tcp
    user            = root
    wait            = no
    redirect        = 192.168.131.253 6443
    port            = 6443
    only_from       = 0.0.0.0/0
    per_source      = UNLIMITED
}

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
    redirect        = 192.168.131.253 443
    port            = 443
    only_from       = 0.0.0.0/0
    per_source      = UNLIMITED
}

service openshift-ingress
{
    flags           = IPv4
    bind            = <host_ip>
    disable         = no
    type            = UNLISTED
    socket_type     = stream
    protocol        = tcp
    user            = root
    wait            = no
    redirect        = 192.168.131.253 80
    port            = 8080
    only_from       = 0.0.0.0/0
    per_source      = UNLIMITED
}
```

###### Restart xinetd

```bash
sudo systemctl restart xinetd
```

###### Ensure 443 and 6443 ports are open

```bash
sudo firewall-cmd --zone=public --permanent --add-service=https
sudo firewall-cmd --permanent --add-port=6443/tcp
sudo firewall-cmd --reload
```

## ACM configuration

See details [here](./env_acm.md).
