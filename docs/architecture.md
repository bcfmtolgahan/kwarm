# Architecture

This document describes the internal architecture of Kwarm.

## Overview

Kwarm consists of three main controllers that work together to manage image pre-pulling:

1. **PrePullPolicy Controller**: Manages policy resources and tracks watched deployments
2. **Deployment Controller**: Watches deployments for image changes
3. **PrePullImage Controller**: Manages the actual pull operations

## Components

### PrePullPolicy Controller

The PrePullPolicy controller is responsible for:

- Validating PrePullPolicy resources
- Counting deployments that match the policy's selector
- Updating policy status with watched deployment counts
- Providing metrics for watched deployments

### Deployment Controller

The Deployment controller watches all Deployments in the cluster and:

1. Checks if each Deployment matches any PrePullPolicy selector
2. Tracks image changes using annotations (`kwarm.io/last-images`)
3. Creates PrePullImage resources when images change
4. Sets owner references so PrePullImages are cleaned up with their Deployments

### PrePullImage Controller

The PrePullImage controller manages the lifecycle of pull operations:

1. **Pending Phase**: Identifies target nodes and initializes status
2. **InProgress Phase**: Creates and monitors pull Jobs
3. **Completed/Failed Phase**: Updates final status and handles cleanup

## Resource Flow

```
PrePullPolicy (Cluster-scoped)
      |
      | defines selector
      v
Deployment (with kwarm.io/enabled label)
      |
      | image change detected
      v
PrePullImage (Namespace-scoped)
      |
      | creates per-node
      v
Jobs (in kwarm-system namespace)
      |
      | pulls image
      v
Image cached on node
```

## Pull Job Design

Each pull job is a Kubernetes Job that:

- Runs on a specific node (using `nodeSelector`)
- Tolerates all taints (to reach any node)
- Uses the target image with a simple exit command
- Has resource limits from the policy
- Includes appropriate imagePullSecrets

The job doesn't actually run the application - it just triggers the image pull. Once the image is pulled, the job exits successfully.

## Rate Limiting

The rate limiter prevents overloading the registry and nodes:

```go
type RateLimiter struct {
    maxConcurrentPulls int    // Cluster-wide limit
    maxPullsPerNode    int    // Per-node limit
    currentPulls       int
    nodeCurrentPulls   map[string]int
}
```

When a pull job is created, it must acquire a slot from the rate limiter. Slots are released when jobs complete or fail.

## Metrics Collection

Metrics are collected at various points:

- **Pull Duration**: Recorded when a job completes
- **Active Pulls**: Updated when jobs start/finish
- **Queue Depth**: Updated during reconciliation
- **Phase Counts**: Updated when PrePullImage phases change

## Retry Logic

Failed pulls are retried with the following logic:

1. Job fails
2. Controller checks retry count against maxAttempts
3. If under limit: delete failed job, reset status to Pending, increment retry count
4. If at limit: mark as Failed, stop retrying

## Cleanup

Resources are cleaned up in several ways:

1. **Owner References**: PrePullImages are owned by Deployments, Jobs are owned by PrePullImages
2. **TTL**: Completed PrePullImages are deleted after the configured TTL
3. **Finalizers**: PrePullImages use finalizers to ensure Jobs are cleaned up before deletion

## Security Considerations

- The operator runs with minimal RBAC permissions
- Pull jobs run as non-root with restricted security contexts
- ImagePullSecrets are handled securely through Kubernetes references
- No secrets are logged or exposed in metrics

## High Availability

For HA deployments:

- Enable leader election (`--leader-elect=true`)
- Deploy multiple replicas
- Only the leader processes reconciliation
- Leader election uses Kubernetes lease objects
