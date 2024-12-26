# Tools to test the alarms server

- A minimal server that can be called to accept Subscriber notification `./server -port 8080`

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
    if r.Method != http.MethodPost {
      http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
      return
    }

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
  })

  addr := fmt.Sprintf(":%d", *port)
  log.Printf("Starting server on %s", addr)
  if err := http.ListenAndServe(addr, nil); err != nil {
    log.Fatal("Server failed to start:", err)
  }
}
```

- A test PromRule that emits alert. Apply to Hub to Spoke. ACM auto applies cluster ID.

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

- A test Thanos rule that is accepted by ACM on Hub and is propagated everywhere.
  Note this is the type of rule that has missing cluster ID.

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

- Generate large alert payload

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
