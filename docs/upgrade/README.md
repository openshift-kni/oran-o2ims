## Example upgrade from 4.16 to 4.17
### starting conditions:
- spoke cluster is successfully deployed with:  
```yaml
// ClusterRequest
spec:
    templateRef:
        name: clustertemplate-a
        version: v1
        namespace: cluster-templates-a-4.16
    release: 4.16
status:
    conditions:
    - status: "True"
      type: ClusterProvisioned
```
### steps:
- CSP creates seedImage compatible with target cluster
- CSP creates a directory and cluster template file for the new release. The `clusterImageSetNameRef` should point to the new release.
```
- version_4.16
    - clustertemplate-a
        - clustertemplate-a-v1.yaml
        - clustertemplate-a-v1-defaults.yaml
- version_4.17
    - clustertemplate-a
        - clustertemplate-a-v1.yaml
        - clustertemplate-a-v1-defaults.yaml
```
- (Optional) SMO creates oadp backup and restore configmaps for application backup.
- SMO triggers the upgrade by updating ClusterRequest's `templateRef` and populating `spec.upgradeSeedImageRef`
```yaml
spec:
    templateRef:
        name: clustertemplate-a
        version: v1
        namespace: cluster-templates-a-4.17
    release: 4.17
    upgrade:
        seedImageRef:
            image: quay.io/seed:latest
            version: 4.17
            pullSecret: 
        oadpContent:
        - cmRefToAppBackup1
        - cmRefToAppBackup2
```
- IMS operators renders the ClusterInstance and detects change to `clusterImageSetNameRef`.
- IMS validates the upgrade by comparing the `clusterImageSetNameRef` to `spec.release`
- IMS creates IBGU with platform backup and application backups
```yaml
  plan:
    - actions: ["Prep", "AbortOnFailure"]
      rolloutStrategy:
        maxConcurrency: 1
        timeout: 15
    - actions: ["Upgrade", "AbortOnFailure", "FinalizeUpgrade"]
      rolloutStrategy:
        maxConcurrency: 1
        timeout: 40
```
- IMS adds a new condition to status
```yaml
      message: Upgrade initiated
      reason: InProgress
      status: "False"         
      type: ClusterUpgraded
```
- IMS monitors IBGU status and updates `ClusterUpgraded` condition accordingly.
```yaml
      message: Prep in progress
      reason: InProgress
      status: "False"         
      type: ClusterUpgraded

      message: Upgrade in progress
      reason: InProgress
      status: "False"         
      type: ClusterUpgraded

      message: Upgrade completed
      reason: Completed
      status: "True"         
      type: ClusterUpgraded

      message: Upgrade failed: failed step, ibgu error message 
      reason: Failed
      status: "False"         
      type: ClusterUpgraded
```
- IMS deletes IBGU after successful upgrade.
- IMS updates `clusterImageSetNameRef` of `ClusterInstance` and adds `AgentClusterInstall` to `suppressedManifests` .
