# qBittorrent Operator

A Kubernetes operator that manages qBittorrent torrents through Custom Resource Definitions (CRDs). This operator allows you to declaratively manage torrents in qBittorrent using native Kubernetes resources.

## Table of Contents

- [How It Works](#how-it-works)
- [Custom Resource Definition](#custom-resource-definition)
- [qBittorrent API Reference](#qbittorrent-api-reference)
- [Installation](#installation)
- [Usage Examples](#usage-examples)
- [Complete Setup Guide](#complete-setup-guide)
- [Configuration](#configuration)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)

## How It Works

The qBittorrent Operator introduces a new Custom Resource Definition (CRD) called `Torrent` that represents a torrent in your qBittorrent instance. The operator watches for changes to these `Torrent` resources and automatically:

1. **Creates torrents** in qBittorrent when new `Torrent` resources are created
2. **Syncs status** from qBittorrent back to the Kubernetes resource
3. **Removes torrents** from qBittorrent when `Torrent` resources are deleted
4. **Maintains consistency** between Kubernetes state and qBittorrent state

### Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   kubectl       │    │  qBittorrent     │    │   qBittorrent   │
│   apply         │───▶│  Operator        │───▶│   Instance      │
│   torrent.yaml  │    │  (Controller)    │    │   (Web API)     │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                              │                          │
                              │                          │
                              ▼                          ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │  Kubernetes API  │    │   Torrent       │
                       │  (Torrent CRD)   │    │   Downloads     │
                       └──────────────────┘    └─────────────────┘
```

### Controller Logic

The operator follows the standard Kubernetes controller pattern:

1. **Watch**: Monitors `Torrent` resources for changes
2. **Reconcile**: Compares desired state (Kubernetes) with actual state (qBittorrent)
3. **Act**: Makes API calls to qBittorrent to align states
4. **Update**: Reports current status back to Kubernetes

## Custom Resource Definition

### Torrent Resource

The `Torrent` CRD defines the schema for managing torrents:

```yaml
apiVersion: torrent.qbittorrent.io/v1alpha1
kind: Torrent
metadata:
  name: my-torrent
  namespace: media-server
spec:
  magnet_uri: "magnet:?xt=urn:btih:example-hash"
status:
  # Read-only fields populated by the operator
  content_path: "/downloads/media/Example Torrent"
  added_on: "1640995200"
  state: "downloading"
  total_size: 1073741824
  name: "Example Torrent"
  time_active: 3600
  amount_left: 536870912
  hash: "8c212779b4abde7c6bc608063a0d008b7e40ce32"
  conditions:
  - type: Available
    status: "True"
    reason: TorrentActive
    message: "Torrent is active in qBittorrent"
    lastTransitionTime: "2024-01-15T10:30:00Z"
```

### Field Descriptions

#### Spec Fields (User-defined)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `magnet_uri` | string | Yes | The magnet URI for the torrent to download |

#### Status Fields (Operator-managed)

| Field | Type | Description |
|-------|------|-------------|
| `content_path` | string | Absolute path where torrent content is stored |
| `added_on` | string | Unix timestamp when torrent was added |
| `state` | string | Current torrent state (see [Torrent States](#torrent-states)) |
| `total_size` | integer | Total size in bytes of all files in the torrent |
| `name` | string | Display name of the torrent |
| `time_active` | integer | Total active time in seconds |
| `amount_left` | integer | Bytes remaining to download |
| `hash` | string | Unique torrent hash identifier |
| `conditions` | array | Standard Kubernetes conditions array |

#### Torrent States

The `state` field can have the following values:

| State | Description |
|-------|-------------|
| `downloading` | Torrent is actively downloading |
| `uploading` | Torrent is seeding (uploading to peers) |
| `pausedDL` | Download is paused |
| `pausedUP` | Upload/seeding is paused |
| `queuedDL` | Queued for download |
| `queuedUP` | Queued for upload |
| `stalledDL` | Download stalled (no peers) |
| `stalledUP` | Upload stalled (no peers) |
| `checkingDL` | Checking download integrity |
| `checkingUP` | Checking upload integrity |
| `error` | Error occurred |
| `missingFiles` | Torrent files are missing |

## qBittorrent API Reference

The operator uses the qBittorrent Web API v2. Key endpoints used:

### Authentication
- `POST /api/v2/auth/login` - Authenticate and get session cookie

### Torrent Management
- `GET /api/v2/torrents/info` - Get list of all torrents
- `POST /api/v2/torrents/add` - Add new torrent via magnet URI
- `POST /api/v2/torrents/delete` - Remove torrent by hash

For complete API documentation, see: [qBittorrent Web API](https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1))

## Installation

### Prerequisites

- Kubernetes cluster (v1.20+)
- kubectl configured
- Docker (for building images)
- Go 1.19+ (for development)

### Install the Operator

#### Option 1: Using Kustomize (Recommended)

```bash
# Clone the repository
git clone https://github.com/guidonguido/qbittorrent-operator
cd qbittorrent-operator

# Deploy using kustomize
kubectl apply -k config/default/
```

#### Option 2: Using Make Targets

```bash
# Clone the repository
git clone https://github.com/guidonguido/qbittorrent-operator
cd qbittorrent-operator

# Install CRDs
make install

# Deploy the operator
make deploy IMG=controller:latest
```

#### Option 3: Using Install Manifest

```bash
# Clone the repository
git clone https://github.com/guidonguido/qbittorrent-operator
cd qbittorrent-operator

# Generate single install file
make build-installer IMG=controller:latest

# Apply the generated manifest
kubectl apply -f dist/install.yaml
```

**Verify installation**:
```bash
kubectl get pods -n qbittorrent-operator
kubectl get crd torrents.torrent.qbittorrent.io
```

### Build from Source

```bash
git clone https://github.com/yourusername/qbittorrent-operator
cd qbittorrent-operator

# Build and deploy
make docker-build IMG=qbittorrent-operator:latest
make deploy IMG=qbittorrent-operator:latest
```

## Usage Examples

### Basic Torrent Management

```yaml
# Create a torrent resource
apiVersion: torrent.qbittorrent.io/v1alpha1
kind: Torrent
metadata:
  name: ubuntu-iso
  namespace: media-server
spec:
  magnet_uri: "magnet:?xt=urn:btih:ubuntu-22.04-desktop-amd64.iso"
```

```bash
# Apply the torrent
kubectl apply -f torrent.yaml

# Check status
kubectl get torrents -n media-server
kubectl describe torrent ubuntu-iso -n media-server

# Delete torrent (removes from qBittorrent)
kubectl delete torrent ubuntu-iso -n media-server
```

### Multiple Torrents

```yaml
apiVersion: torrent.qbittorrent.io/v1alpha1
kind: Torrent
metadata:
  name: linux-distros
  namespace: media-server
  labels:
    category: "operating-systems"
spec:
  magnet_uri: "magnet:?xt=urn:btih:debian-12-amd64-netinst.iso"
---
apiVersion: torrent.qbittorrent.io/v1alpha1
kind: Torrent
metadata:
  name: fedora-iso
  namespace: media-server
  labels:
    category: "operating-systems"
spec:
  magnet_uri: "magnet:?xt=urn:btih:fedora-39-x86_64-netinst.iso"
```

### Monitoring Torrent Progress

```bash
# Watch torrent status in real-time
kubectl get torrents -n media-server -w

# Get detailed status
kubectl get torrent ubuntu-iso -n media-server -o yaml

# Check operator logs
kubectl logs -f deployment/qbittorrent-operator-controller-manager -n qbittorrent-operator-system
```

## Complete Setup Guide

### Step 1: Deploy qBittorrent

First, deploy qBittorrent with VPN protection:

```yaml
# qbittorrent-namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: media-server
---
# qbittorrent-pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: qbittorrent
  namespace: media-server
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: media-pvc
  namespace: media-server
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
---
# qbittorrent-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: qbittorrent
  namespace: media-server
  labels:
    app: qbittorrent
    part-of: qbittorrent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: qbittorrent
      part-of: qbittorrent
  template:
    metadata:
      labels:
        app: qbittorrent
        part-of: qbittorrent
    spec:
      containers:
      - name: qbittorrent
        image: lscr.io/linuxserver/qbittorrent:latest
        ports:
        - name: qbittorrent
          containerPort: 8080
        env:
        - name: PUID
          value: "0"
        - name: PGID
          value: "0"
        - name: UMASK
          value: "022"
        volumeMounts:
        - name: qbittorrent
          mountPath: /config
        - name: downloads-media
          mountPath: /downloads/media
        resources:
          limits:
            memory: 250Mi
          requests:
            memory: 200Mi
      restartPolicy: Always
      volumes:
      - name: qbittorrent
        persistentVolumeClaim:
          claimName: qbittorrent
      - name: downloads-media
        persistentVolumeClaim:
          claimName: media-pvc
---
# qbittorrent-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: qbittorrent
  namespace: media-server
spec:
  selector:
    app: qbittorrent
    part-of: qbittorrent
  type: ClusterIP
  ports:
    - port: 8080
      targetPort: qbittorrent
```

### Step 2: Configure qBittorrent

1. **Port forward to access Web UI**:
```bash
kubectl port-forward svc/qbittorrent 8080:8080 -n media-server
```

2. **Access Web UI**: Open http://localhost:8080
   - Default username: `admin`
   - Get password from logs: `kubectl logs deployment/qbittorrent -n media-server`

3. **Configure qBittorrent**:
   - Go to Tools → Options → Web UI
   - Set username/password for API access
   - Note the credentials for operator configuration

### Step 3: Deploy the Operator

1. **Create operator configuration**:
```yaml
# operator-config.yaml
apiVersion: v1
kind: Secret
metadata:
  name: qbittorrent-secret
  namespace: qbittorrent-operator
type: Opaque
data:
  username: <base64-encoded-qbittorrent-username>
  password: <base64-encoded-qbittorrent-password>
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: qbittorrent-config
  namespace: qbittorrent-operator
data:
  url: "http://qbittorrent.media-server.svc.cluster.local:8080"
```

2. **Deploy the operator**:
```bash
# Apply configuration
kubectl apply -f operator-config.yaml

# Deploy operator
make deploy IMG=qbittorrent-operator:latest
```

### Step 4: Test the Setup

```yaml
# test-torrent.yaml
apiVersion: torrent.qbittorrent.io/v1alpha1
kind: Torrent
metadata:
  name: test-torrent
  namespace: media-server
spec:
  magnet_uri: "magnet:?xt=urn:btih:ubuntu-22.04-desktop-amd64.iso"
```

```bash
# Apply test torrent
kubectl apply -f test-torrent.yaml

# Monitor progress
kubectl get torrent test-torrent -n media-server -w

# Check in qBittorrent Web UI
# The torrent should appear automatically
```

## Configuration

### Operator Configuration

The operator can be configured via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `QBITTORRENT_URL` | qBittorrent Web UI URL | Required |
| `QBITTORRENT_USERNAME` | qBittorrent username | Required |
| `QBITTORRENT_PASSWORD` | qBittorrent password | Required |

### qBittorrent Configuration

For optimal operation with the operator:

1. **Enable Web UI**: Tools → Options → Web UI → Enable
2. **Set Authentication**: Configure username/password
3. **Allow Cross-Origin**: Set to allow API access
4. **Download Path**: Configure default download location

## Monitoring

### Metrics

The operator exposes Prometheus metrics on `:8080/metrics`:

- `controller_runtime_reconcile_total` - Total reconciliations
- `controller_runtime_reconcile_errors_total` - Reconciliation errors
- `controller_runtime_reconcile_time_seconds` - Reconciliation duration

### Logging

Operator logs include structured information:

```bash
# View operator logs
kubectl logs -f deployment/qbittorrent-operator-controller-manager -n qbittorrent-operator-system

# Increase log verbosity
kubectl patch deployment qbittorrent-operator-controller-manager -n qbittorrent-operator-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--leader-elect","--health-probe-bind-address=:8081","--v=2"]}]}}}}'
```

### Health Checks

The operator provides health endpoints:

- `/healthz` - Liveness probe
- `/readyz` - Readiness probe (includes qBittorrent connectivity)

## Troubleshooting

### Common Issues

#### 1. Operator Can't Connect to qBittorrent

```bash
# Check operator logs
kubectl logs deployment/qbittorrent-operator-controller-manager -n qbittorrent-operator-system

# Common causes:
# - Wrong URL in configuration
# - qBittorrent not running
# - Network policies blocking access
# - Incorrect credentials
```

#### 2. Torrents Not Appearing in qBittorrent

```bash
# Check torrent resource status
kubectl describe torrent <torrent-name> -n <namespace>

# Look for conditions:
# - Available: True = Working correctly
# - Degraded: True = Error occurred (check message)
```

#### 3. Torrents Not Syncing Status

```bash
# Check reconciliation frequency
kubectl get torrent <torrent-name> -n <namespace> -o yaml

# Status should update every 30 seconds
# If not updating, check operator logs for errors
```

### Debug Commands

```bash
# List all torrents across namespaces
kubectl get torrents --all-namespaces

# Get detailed torrent information
kubectl get torrent <name> -n <namespace> -o yaml

# Check operator events
kubectl get events -n qbittorrent-operator-system

# Port forward to qBittorrent for direct access
kubectl port-forward svc/qbittorrent 8080:8080 -n media-server
```

### Log Analysis

Look for these log patterns:

```
# Successful reconciliation
"Successfully logged into qBittorrent"
"Torrent reconciliation completed"

# Connection issues
"Failed to login to qBittorrent"
"Failed to get torrents info list"

# API errors
"Unauthorized access to qbittorrent"
"Failed to add torrent to qBittorrent"
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

