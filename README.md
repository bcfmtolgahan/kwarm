# Kwarm

Kubernetes Image Pre-pull Operator. Pre-pulls container images to nodes before deployments roll out.

## Overview

Kwarm watches Deployments for image changes and automatically pulls new images to target nodes before the rolling update begins. This eliminates image pull latency during deployments.

## Installation

### Helm

```bash
helm install kwarm ./charts/kwarm -n kwarm-system --create-namespace
```

### Kubectl

```bash
kubectl apply -k config/default
```

## Usage

### 1. Create a PrePullPolicy

```yaml
apiVersion: kwarm.io/v1alpha1
kind: PrePullPolicy
metadata:
  name: default
spec:
  selector:
    matchLabels:
      kwarm.io/enabled: "true"
  nodeSelector:
    matchLabels: {}
  resources:
    requests:
      memory: "64Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "500m"
  retry:
    maxAttempts: 3
    backoffSeconds: 30
  timeout: "10m"
```

### 2. Label your Deployments

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  labels:
    kwarm.io/enabled: "true"
spec:
  template:
    spec:
      containers:
      - name: app
        image: my-registry/my-app:v2
```

When you update the image tag, Kwarm automatically:
1. Detects the image change
2. Creates a PrePullImage resource
3. Runs pull jobs on target nodes
4. Images are cached before pods are scheduled

## Configuration

### PrePullPolicy Spec

| Field | Description | Default |
|-------|-------------|---------|
| `selector.matchLabels` | Labels to match Deployments | required |
| `selector.namespaces` | Limit to specific namespaces | all |
| `nodeSelector.matchLabels` | Target specific nodes | all ready nodes |
| `resources` | Resource limits for pull jobs | 64Mi/100m |
| `retry.maxAttempts` | Max retry attempts | 3 |
| `retry.backoffSeconds` | Backoff between retries | 30 |
| `timeout` | Pull timeout per node | 10m |
| `imagePullSecrets.inherit` | Inherit secrets from Deployment | false |
| `imagePullSecrets.additional` | Additional pull secrets | [] |

### Operator Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--max-concurrent-pulls` | Max concurrent pulls cluster-wide | 10 |
| `--max-pulls-per-node` | Max concurrent pulls per node | 2 |
| `--metrics-bind-address` | Metrics endpoint address | :8080 |
| `--health-probe-bind-address` | Health probe address | :8081 |

## Metrics

Prometheus metrics available at `:8080/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `kwarm_pull_duration_seconds` | Histogram | Image pull duration |
| `kwarm_active_pulls` | Gauge | Current active pulls |
| `kwarm_pulls_total` | Counter | Total pulls by status |
| `kwarm_prepullimages_by_phase` | Gauge | PrePullImages by phase |
| `kwarm_watched_deployments` | Gauge | Watched deployments per policy |

## Architecture

```
PrePullPolicy (cluster-scoped)
      |
      | defines selector
      v
Deployment (with kwarm.io/enabled label)
      |
      | image change detected
      v
PrePullImage (namespace-scoped)
      |
      | creates per-node
      v
Jobs (in kwarm-system)
      |
      | pulls image
      v
Image cached on node
```

## Development

```bash
# Run locally
make run

# Run tests
make test

# Build image
make docker-build IMG=kwarm:dev

# Deploy to cluster
make deploy IMG=kwarm:dev
```

## License

Apache License 2.0
