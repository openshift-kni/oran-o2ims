<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Tools to test the alarms server

## Minimal CRs needed to run ACM's observability stack

1. S3 compatible minio deployment that you can apply to Hub cluster (i.e the same cluster where ACM observability stack will run)

    ```yaml
    apiVersion: v1
    kind: Namespace
    metadata:
      name: minio-dev
    ---
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata:
      name: minio-pv-claim
      namespace: minio-dev
      labels:
        app: minio-storage-claim
    spec:
      accessModes:
        - ReadWriteOnce
      resources:
        requests:
          storage: 10Gi
    ---
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: minio-deployment
      namespace: minio-dev
    spec:
      selector:
        matchLabels:
          app: minio
      strategy:
        type: Recreate
      template:
        metadata:
          labels:
            app: minio
        spec:
          volumes:
            - name: storage
              persistentVolumeClaim:
                claimName: minio-pv-claim
            - name: bucket-script
              configMap:
                name: minio-bucket-script
                defaultMode: 0755
          containers:
            - name: minio
              image: quay.io/minio/minio:latest
              args:
                - server
                - /storage
                - --console-address
                - :9090
              env:
                - name: MINIO_ROOT_USER
                  value: "minio"
                - name: MINIO_ROOT_PASSWORD
                  value: "minio123"
              ports:
                - containerPort: 9000
                  name: api
                - containerPort: 9090
                  name: console
              volumeMounts:
                - name: storage
                  mountPath: "/storage"
                - name: bucket-script
                  mountPath: "/scripts"
              lifecycle:
                postStart:
                  exec:
                    command: ["/bin/sh", "/scripts/create-bucket.sh"]
    ---
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: minio-bucket-script
      namespace: minio-dev
    data:
      create-bucket.sh: |
        #!/bin/sh
        sleep 10
        mkdir -p /storage/thanos
        echo "Bucket 'thanos' created."
    ---
    apiVersion: v1
    kind: Service
    metadata:
      name: minio
      namespace: minio-dev
    spec:
      ports:
        - port: 9000
          targetPort: 9000
          protocol: TCP
          name: api
        - port: 9090
          targetPort: 9090
          protocol: TCP
          name: console
      selector:
        app: minio
    ```

2. Enable ACM observability stack

   Create `open-cluster-management-observability` namespace and apply the following. Note the S3 config.

    ```yaml
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
          bucket: thanos
          endpoint: minio.minio-dev.svc:9000
          insecure: true
          access_key: minio
          secret_key: minio123
    ---
    apiVersion: observability.open-cluster-management.io/v1beta2
    kind: MultiClusterObservability
    metadata:
      name: observability
    spec:
      advanced:
        alertmanager:
          replicas: 1 # required due a known ACM issue CNF-16350
      observabilityAddonSpec: {}
      storageConfig:
        metricObjectStorage:
          name: thanos-object-storage
          key: thanos.yaml
    ```

## A minimal server that can be called to accept Subscriber notification

`./server -port 8080`

```go
package main

import (
   "encoding/json"
   "flag"
   "fmt"
   "log"
   "net/http"
   "sync/atomic"
)

var count atomic.Int64

func main() {
   // Define command-line flag for port
   port := flag.Int("port", 8080, "Port to listen on")
   flag.Parse()

   http.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
      switch r.Method {
      case http.MethodGet:
         // Handle reachability check: respond with 204 No Content.
         w.WriteHeader(http.StatusNoContent)
         log.Printf("Received reachability GET request")
         return
      case http.MethodPost:
         // Process the notification as usual.
         var payload interface{}
         if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
         }

         prettyJSON, _ := json.MarshalIndent(payload, "", "  ")
         log.Printf("Received notification:\n%s\n", string(prettyJSON))
         w.WriteHeader(http.StatusOK)
         log.Printf("Notification received successfully")

         if payloadList, ok := payload.([]interface{}); ok {
            log.Printf("Number of items in list: %d", len(payloadList))
         }

         newCount := count.Add(1)
         log.Printf("Total calls so far: %d", newCount)
      default:
         http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
      }
   })

   addr := fmt.Sprintf(":%d", *port)
   log.Printf("Starting server on %s", addr)
   if err := http.ListenAndServe(addr, nil); err != nil {
      log.Fatal("Server failed to start:", err)
   }
}
```

## A test PromRule that emits alert. Apply to Hub to Spoke. ACM auto applies cluster ID

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: fake-alerting-rules
  namespace: open-cluster-management-addon-observability
spec:
  groups:
    - name: ./example.rules
      rules:
        - alert: ExampleAlert
          expr: vector(1)
          labels:
            severity: major
```

## A test Thanos rule that is accepted by ACM on Hub and is propagated everywhere

Note this is the type of rule that has missing cluster ID (CNF-16632)

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: thanos-ruler-custom-rules
  namespace: open-cluster-management-observability
data:
  custom_rules.yaml: |
    groups:
    - name: node-health
      rules:
      - alert: NodeOutOfMemoryFakeButFromAllNodes
        expr: instance:node_memory_utilisation:ratio * 100 > 0
        for: 1m
        labels:
          instance: "{{ $labels.instance }}"
          cluster: "{{ $labels.cluster }}"
          clusterID: "{{ $labels.clusterID }}"
          severity: warning
```

## Generate large alert payload

```python
import json
import random
import uuid
from datetime import datetime, timedelta

base_time = datetime(2025, 1, 3, 10, 0, 0)
alerts = []

for i in range(3500):
    start_time = base_time + timedelta(minutes=random.randint(0, 1440))  # Random time within 24 hours
    end_time = start_time + timedelta(minutes=30)

    alerts.append({
        "status": "firing",
        "labels": {
            "alertname": "high_cpu_usage",
            "instance": f"server-{str(i).zfill(5)}",
            "severity": "critical",
            "value": str(random.randint(90, 99)),
            "managed_cluster": "17134c55-4ab0-44c6-8469-f53d298ca672"
        },
        "annotations": {
            "description": f"CPU usage is {random.randint(90, 99)}% on server-{str(i).zfill(5)}"
        },
        "startsAt": start_time.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "endsAt": end_time.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "fingerprint": str(uuid.uuid4()),
        "generatorURL": "https://prometheus.example.com/graph?query=cpu_usage"
    })

payload = {
    "version": "4",
    "groupKey": "{}:{alertname=\"high_cpu_usage\"}",
    "status": "firing",
    "receiver": "team-cpu-alerts",
    "alerts": alerts
}

with open('alerts.json', 'w') as f:
    json.dump(payload, f, indent=4)
```
