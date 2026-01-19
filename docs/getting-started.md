# Getting Started

This guide will help you install and configure Kwarm on your Kubernetes cluster.

## Prerequisites

- Kubernetes 1.28+
- Helm 3.0+ (for Helm installation)
- kubectl configured to communicate with your cluster

## Installation

### Using Helm (Recommended)

```bash
# Add the Helm repository
helm repo add kwarm https://kwarm.github.io/kwarm
helm repo update

# Install Kwarm
helm install kwarm kwarm/kwarm \
  --namespace kwarm-system \
  --create-namespace
```

### Using kubectl

```bash
kubectl apply -f https://github.com/kwarm/kwarm/releases/latest/download/install.yaml
```

## Verify Installation

```bash
# Check if the operator is running
kubectl get pods -n kwarm-system

# Check if CRDs are installed
kubectl get crd | grep kwarm.io
```

## Create Your First Policy

Create a file named `policy.yaml`:

```yaml
apiVersion: kwarm.io/v1alpha1
kind: PrePullPolicy
metadata:
  name: default-policy
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
  retry:
    maxAttempts: 3
    backoffSeconds: 30
  timeout: "10m"
  imagePullSecrets:
    inherit: true
```

Apply it:

```bash
kubectl apply -f policy.yaml
```

## Enable Pre-pulling on a Deployment

Add the label `kwarm.io/enabled: "true"` to any Deployment you want to monitor:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  labels:
    kwarm.io/enabled: "true"
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: nginx:1.25
```

## Monitor Pre-pulling

When you update the image in your Deployment, Kwarm will automatically create a PrePullImage resource:

```bash
# List PrePullImages
kubectl get prepullimages

# Watch the status
kubectl get prepullimages -w

# Get detailed status
kubectl describe prepullimage <name>
```

## View Metrics

If metrics are enabled, you can access them at the metrics endpoint:

```bash
kubectl port-forward -n kwarm-system svc/kwarm-metrics 8080:8080
curl http://localhost:8080/metrics
```

## Next Steps

- Read the [Configuration Guide](configuration.md) for advanced options
- Learn about the [Architecture](architecture.md)
- Check out the [Troubleshooting Guide](troubleshooting.md)
