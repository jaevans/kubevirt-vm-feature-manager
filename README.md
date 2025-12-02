# KubeVirt VM Feature Manager

A mutating admission webhook for Harvester HCI that enables advanced features on KubeVirt VirtualMachine objects through simple annotations or labels.

## Features

- **Nested Virtualization**: Enable nested virtualization (AMD SVM / Intel VMX) for VMs
- **vBIOS Injection**: Inject custom vBIOS blobs for GPU passthrough (via hook sidecar)
- **PCI Passthrough**: Configure PCI device passthrough
- **GPU Device Plugin**: Attach GPUs via Kubernetes device plugins
- **Flexible Configuration**: Read feature configuration from annotations (default) or labels

## Quick Start

### Annotations

Add annotations to your VirtualMachine to enable features:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: my-vm
  annotations:
    # Enable nested virtualization
    vm-feature-manager.io/nested-virt: "enabled"
    
    # Enable vBIOS injection with PCI passthrough
    vm-feature-manager.io/vbios-configmap: "my-igpu-vbios"
    vm-feature-manager.io/pci-passthrough: "0000:00:02.0"
    
    # Or use GPU device plugin
    vm-feature-manager.io/gpu-device-plugin: "kubevirt.io/integrated-gpu"
spec:
  # ... rest of VM spec
```

### Userdata Directives (Rancher/Harvester)

For environments where VM annotations aren't accessible (like Rancher/Harvester), you can use **userdata directives** in cloud-init:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: my-vm
spec:
  template:
    spec:
      volumes:
        - name: cloudinit
          cloudInitNoCloud:
            userData: |
              #cloud-config
              # @kubevirt-feature: nested-virt=enabled
              # @kubevirt-feature: gpu-device-plugin=nvidia.com/gpu
              # @kubevirt-feature: pci-passthrough={"devices":["0000:00:02.0"]}
              users:
                - name: ubuntu
                  sudo: ALL=(ALL) NOPASSWD:ALL
```

**Supported formats:**
- Plain text: `userData: |`
- Base64: `userDataBase64: <base64-encoded>`
- Secret reference: `userDataSecretRef: {name: my-secret}`

Security:
- The webhook reads referenced Secrets in the VM namespace for userdata without additional labels or annotations.
- Recommendation: Use namespace-scoped RBAC to limit which secrets are readable; if you can create a VM in the namespace, you are assumed to have permission to read its referenced Secret.

**Note:** VM annotations take precedence over userdata directives.

### Installation

#### Using Helm (Recommended)

```bash
helm install vm-feature-manager oci://ghcr.io/jaevans/kubevirt-vm-feature-manager/charts/vm-feature-manager \
  --namespace kubevirt \
  --create-namespace
```

**From Git Repository:**

```bash
git clone https://github.com/jaevans/kubevirt-vm-feature-manager.git
cd kubevirt-vm-feature-manager

helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace kubevirt \
  --create-namespace
```

### Upgrading

To upgrade to a newer version:

```bash
# Upgrade to the latest version
helm upgrade vm-feature-manager oci://ghcr.io/jaevans/kubevirt-vm-feature-manager/charts/vm-feature-manager \
  --namespace kubevirt

# Upgrade to a specific version
helm upgrade vm-feature-manager oci://ghcr.io/jaevans/kubevirt-vm-feature-manager/charts/vm-feature-manager \
  --namespace kubevirt \
  --version 0.5.0
```

To change configuration during upgrade:

```bash
helm upgrade vm-feature-manager oci://ghcr.io/jaevans/kubevirt-vm-feature-manager/charts/vm-feature-manager \
  --namespace kubevirt \
  --set logLevel=debug \
  --set replicaCount=3
```

### Uninstalling

```bash
helm uninstall vm-feature-manager --namespace kubevirt
```

#### Using Container Images

Multi-arch container images are available on GitHub Container Registry:

```bash
# Pull the latest release
docker pull ghcr.io/jaevans/kubevirt-vm-feature-manager:latest

# Or a specific version
docker pull ghcr.io/jaevans/kubevirt-vm-feature-manager:v1.0.0
```

Supported architectures: `linux/amd64`, `linux/arm64`, `linux/arm/v7`

#### Using Pre-built Binaries

Download pre-built binaries from the [releases page](https://github.com/jaevans/kubevirt-vm-feature-manager/releases).

## Configuration

The webhook can be configured via environment variables or a ConfigMap. See [Configuration](docs/configuration.md) for details.

### Using Labels Instead of Annotations

By default, the webhook reads feature configuration from annotations. If your environment doesn't propagate annotations (e.g., Rancher MachineConfig), you can configure the webhook to read from labels instead:

```bash
helm install vm-feature-manager oci://ghcr.io/jaevans/kubevirt-vm-feature-manager/charts/vm-feature-manager \
  --namespace kubevirt \
  --create-namespace \
  --set configSource=labels
```

Then use labels on your VirtualMachine:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: my-vm
  labels:
    vm-feature-manager.io/nested-virt: "enabled"
    vm-feature-manager.io/gpu-device-plugin: "nvidia.com/gpu"
spec:
  # ... rest of VM spec
```

## Architecture

The webhook uses the KubeVirt hook sidecar pattern for vBIOS injection, allowing it to modify the libvirt domain XML at VM start time.

## Development

### Prerequisites

- Go 1.25+
- Kubernetes cluster with KubeVirt installed
- kubectl

### Building

```bash
# Build locally
make build

# Build multi-arch release locally (requires GoReleaser)
make release-snapshot

# Traditional Docker build
make docker-build
```

See [RELEASE.md](RELEASE.md) for release process documentation.

### Running Locally

```bash
export PORT=8443
export CERT_DIR=./certs
export LOG_LEVEL=debug
./webhook
```

## License

AGPL 3.0 or later

## Contributing

Contributions welcome! Please open an issue or PR.
