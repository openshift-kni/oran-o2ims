# O2IMS

This project project is an implementation of the O-RAN O2 IMS API on top of
OpenShift and ACM.

Note that at this point this is just experimental and at its very beginnings,
so don't try to use it for anything close to a production environment.

***Note: this README is only for development purposes.***

## Quick Start

### Build binary
``` bash
make binary
```

### Run

#### Commands
* start deployment-manager-server

#### Flags
* --backend-token string   Token for authenticating to the backend server.
* --backend-url string     URL of the backend server.
* --cloud-id string        O-Cloud identifier.

#### Set Env Variables
``` bash
export CLOUD_ID=<cloud_id>
export BACKEND_URL=<backend_url>
export BACKEND_TOKEN=<backend_token>
```

#### Using CLI
```bash
./o2ims start deployment-manager-server --cloud-id $CLOUD_ID --backend-url $BACKEND_URL --backend-token $BACKEND_TOKEN
```

#### Using VS Code

`Run and Debug` with the `start deployment-manager-server` [configuration](.vscode/launch.json).

### Usage

#### Examples

##### GET Deployment Manager List 
```bash
curl http://localhost:8080/O2ims_infrastructureInventory/1.2.3/deploymentManagers
```
