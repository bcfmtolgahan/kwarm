# Configuration

This guide covers all configuration options for Kwarm.

## PrePullPolicy Configuration

### Selector

The `selector` field determines which Deployments are watched by the policy.

```yaml
spec:
  selector:
    # Label selector - Deployments must have ALL these labels
    matchLabels:
      kwarm.io/enabled: "true"
      environment: production

    # Namespace filter - only watch these namespaces
    # Empty list means all namespaces
    namespaces:
      - production
      - staging
```

### Node Selector

The `nodeSelector` field determines which nodes receive the pre-pulled images.

```yaml
spec:
  nodeSelector:
    matchLabels:
      # Only pre-pull to nodes with these labels
      node-type: worker
      zone: us-east-1a
```

### Resources

Resource limits for the pull jobs:

```yaml
spec:
  resources:
    requests:
      memory: "64Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "500m"
```

### Retry Policy

Configure how failed pulls are retried:

```yaml
spec:
  retry:
    # Maximum number of retry attempts per node
    maxAttempts: 3
    # Seconds to wait between retries
    backoffSeconds: 30
```

### Timeout

Maximum time allowed for each pull operation:

```yaml
spec:
  timeout: "10m"  # Default: 10 minutes
```

### ImagePullSecrets

Configure how image pull secrets are handled:

```yaml
spec:
  imagePullSecrets:
    # Inherit secrets from the source Deployment
    inherit: true

    # Additional secrets to use (in addition to inherited ones)
    additional:
      - name: private-registry-secret
      - name: another-secret
```

## Operator Configuration

### Command-line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--max-concurrent-pulls` | 10 | Maximum concurrent pulls cluster-wide |
| `--max-pulls-per-node` | 2 | Maximum concurrent pulls per node |
| `--metrics-bind-address` | :8080 | Metrics endpoint address |
| `--metrics-secure` | true | Use HTTPS for metrics |
| `--health-probe-bind-address` | :8081 | Health probe address |
| `--leader-elect` | false | Enable leader election |

### Helm Values

```yaml
# Replica count
replicaCount: 1

# Image configuration
image:
  repository: ghcr.io/kwarm/kwarm
  pullPolicy: IfNotPresent
  tag: ""  # Defaults to chart appVersion

# Operator configuration
config:
  maxConcurrentPulls: 10
  maxPullsPerNode: 2
  defaultTimeout: "10m"
  completedTTL: "1h"
  logLevel: "info"

# Leader election
leaderElection:
  enabled: true

# Resource limits for the operator
resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 100m
    memory: 64Mi

# Metrics
metrics:
  enabled: true
  port: 8080
  secure: false

# Health probes
healthProbe:
  port: 8081

# Pod security
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
```

## Rate Limiting

Kwarm implements rate limiting to prevent overwhelming your container registry or nodes:

- **Cluster-wide limit**: Maximum number of concurrent pull jobs across the entire cluster
- **Per-node limit**: Maximum number of concurrent pull jobs on a single node

These limits help:
- Prevent registry rate limiting
- Avoid network congestion
- Reduce impact on node performance

## TTL for Completed Resources

Completed PrePullImage resources are automatically cleaned up after the TTL expires (default: 1 hour). This prevents accumulation of completed resources in the cluster.

## Private Registries

For private registries, ensure the appropriate imagePullSecrets are configured either:

1. On the source Deployment (with `inherit: true` in the policy)
2. In the policy's `additional` secrets list
3. As default secrets in the service account

The pull jobs run in the `kwarm-system` namespace, so secrets must be available there or the operator must have permissions to read secrets from other namespaces.
