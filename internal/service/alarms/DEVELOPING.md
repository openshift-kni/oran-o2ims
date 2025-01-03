# Tools to test the alarms server

- A minimal server that can be called to accept Subscriber notification

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
)

func main() {
    http.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        // Decode to interface{} to accept any JSON structure
        var payload interface{}
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        // Pretty print the received payload
        prettyJSON, _ := json.MarshalIndent(payload, "", "  ")
        log.Printf("Received notification:\n%s\n", string(prettyJSON))

        w.WriteHeader(http.StatusOK)
        fmt.Fprintf(w, "Notification received successfully")
    })

    log.Println("Starting server on :8080")
    if err := http.ListenAndServe(":8080", nil); err != nil {
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
