# KubeVirt VM Feature Manager

A mutating admission webhook for Harvester HCI that enables advanced features on KubeVirt VirtualMachine objects through simple annotations.

## Features

- **Nested Virtualization**: Enable nested virtualization (AMD SVM / Intel VMX) for VMs
- **vBIOS Injection**: Inject custom vBIOS blobs for GPU passthrough (via hook sidecar)
- **PCI Passthrough**: Configure PCI device passthrough
- **GPU Device Plugin**: Attach GPUs via Kubernetes device plugins

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
    vm-feature-manager.io/nested-virtualization: "enabled"
    
    # Enable vBIOS injection with PCI passthrough
    vm-feature-manager.io/vbios-configmap: "my-igpu-vbios"
    vm-feature-manager.io/pci-passthrough: "0000:00:02.0"
    
    # Or use GPU device plugin
    vm-feature-manager.io/gpu-device-plugin: "kubevirt.io/integrated-gpu"
spec:
  # ... rest of VM spec
```

### Installation

Install via Helm:

```bash
helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace kubevirt \
  --create-namespace
```

## Configuration

The webhook can be configured via environment variables or a ConfigMap. See [Configuration](docs/configuration.md) for details.

## Architecture

The webhook uses the KubeVirt hook sidecar pattern for vBIOS injection, allowing it to modify the libvirt domain XML at VM start time.

## Development

### Prerequisites

- Go 1.23+
- Kubernetes cluster with KubeVirt installed
- kubectl

### Building

```bash
go build -o webhook cmd/webhook/main.go
```

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
