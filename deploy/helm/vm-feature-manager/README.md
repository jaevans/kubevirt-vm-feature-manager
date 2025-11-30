# VM Feature Manager Helm Chart

This Helm chart deploys the KubeVirt VM Feature Manager, a mutating admission webhook that automatically configures VirtualMachine features based on annotations.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- KubeVirt installed in the cluster
- cert-manager (optional but recommended for automatic certificate management)

## Installation Methods

### From OCI Registry (Recommended)

```bash
helm install vm-feature-manager oci://ghcr.io/jaevans/kubevirt-vm-feature-manager/charts/vm-feature-manager \
  --namespace vm-feature-manager \
  --create-namespace
```

### From Git Repository

```bash
git clone https://github.com/jaevans/kubevirt-vm-feature-manager.git
cd kubevirt-vm-feature-manager

helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace vm-feature-manager \
  --create-namespace
```

## Installing the Chart

### With cert-manager (Recommended)

1. Install cert-manager if not already installed:
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

2. Install the chart (it will automatically create a self-signed Issuer):
```bash
helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace vm-feature-manager \
  --create-namespace
```

The chart will automatically create a self-signed `Issuer` in the namespace to provision certificates. No additional setup required!

### With an existing cert-manager Issuer

If you want to use an existing ClusterIssuer or Issuer:

```bash
helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace vm-feature-manager \
  --create-namespace \
  --set certificates.certManager.createIssuer=false \
  --set certificates.certManager.issuerKind=ClusterIssuer \
  --set certificates.certManager.issuerName=my-existing-issuer
```

### Without cert-manager (Manual Certificates)

1. Generate certificates:
```bash
# Generate CA
openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -subj "/CN=vm-feature-manager-ca" -days 3650 -out ca.crt

# Generate server certificate
openssl genrsa -out tls.key 2048
openssl req -new -key tls.key -subj "/CN=vm-feature-manager-webhook.vm-feature-manager.svc" -out tls.csr

# Sign the certificate
cat > csr.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = vm-feature-manager-webhook
DNS.2 = vm-feature-manager-webhook.vm-feature-manager
DNS.3 = vm-feature-manager-webhook.vm-feature-manager.svc
DNS.4 = vm-feature-manager-webhook.vm-feature-manager.svc.cluster.local
EOF

openssl x509 -req -in tls.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out tls.crt -days 365 -extensions v3_req -extfile csr.conf

# Base64 encode
CA_BUNDLE=$(cat ca.crt | base64 -w 0)
TLS_CRT=$(cat tls.crt | base64 -w 0)
TLS_KEY=$(cat tls.key | base64 -w 0)
```

2. Install with manual certificates:
```bash
helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace vm-feature-manager \
  --create-namespace \
  --set certificates.certManager.enabled=false \
  --set certificates.manual.caCert="$CA_BUNDLE" \
  --set certificates.manual.tlsCert="$TLS_CRT" \
  --set certificates.manual.tlsKey="$TLS_KEY"
```

## Configuration

The following table lists the configurable parameters of the chart and their default values.

| Parameter                               | Description                          | Default                                       |
| --------------------------------------- | ------------------------------------ | --------------------------------------------- |
| `replicaCount`                          | Number of webhook replicas           | `2`                                           |
| `image.repository`                      | Webhook image repository             | `ghcr.io/jaevans/kubevirt-vm-feature-manager` |
| `image.tag`                             | Image tag                            | Chart appVersion                              |
| `image.pullPolicy`                      | Image pull policy                    | `IfNotPresent`                                |
| `webhook.port`                          | Webhook server port                  | `8443`                                        |
| `webhook.certDir`                       | Certificate directory                | `/etc/webhook/certs`                          |
| `webhook.failurePolicy`                 | Webhook failure policy (Fail/Ignore) | `Fail`                                        |
| `webhook.timeoutSeconds`                | Webhook timeout                      | `10`                                          |
| `certificates.certManager.enabled`      | Use cert-manager                     | `true`                                        |
| `certificates.certManager.createIssuer` | Create a self-signed Issuer          | `true`                                        |
| `certificates.certManager.issuerKind`   | Issuer kind (if createIssuer=false)  | `ClusterIssuer`                               |
| `certificates.certManager.issuerName`   | Issuer name (if createIssuer=false)  | `my-cluster-issuer`                           |
| `errorHandling.mode`                    | Error handling mode                  | `StripLabel`                                  |
| `resources.limits.cpu`                  | CPU limit                            | `200m`                                        |
| `resources.limits.memory`               | Memory limit                         | `128Mi`                                       |
| `resources.requests.cpu`                | CPU request                          | `100m`                                        |
| `resources.requests.memory`             | Memory request                       | `64Mi`                                        |

## Features

The webhook supports the following annotations on VirtualMachine objects:

### Nested Virtualization
```yaml
metadata:
  annotations:
    vm-feature-manager.io/nested-virt: "enabled"
```

### GPU Device Plugin
```yaml
metadata:
  annotations:
    vm-feature-manager.io/gpu-device-plugin: "nvidia.com/gpu"
```

### vBIOS Injection
```yaml
metadata:
  annotations:
    vm-feature-manager.io/vbios-injection: "my-vbios-configmap"
```

### PCI Passthrough
```yaml
metadata:
  annotations:
    vm-feature-manager.io/pci-passthrough: "0000:00:02.0"
```

## Uninstalling the Chart

```bash
helm uninstall vm-feature-manager --namespace vm-feature-manager
```

## Troubleshooting

### Check webhook logs:
```bash
kubectl logs -n vm-feature-manager -l app.kubernetes.io/name=vm-feature-manager
```

### Check certificate status (with cert-manager):
```bash
kubectl get certificate -n vm-feature-manager
kubectl describe certificate -n vm-feature-manager vm-feature-manager-cert
```

### Test webhook manually:
```bash
kubectl run test-vm --image=nginx --dry-run=server -o yaml
```

### Verify webhook is receiving requests:
```bash
kubectl logs -n vm-feature-manager -l app.kubernetes.io/name=vm-feature-manager --tail=100 -f
```

## Development

To test changes locally:

```bash
# Lint the chart
helm lint ./deploy/helm/vm-feature-manager

# Template the chart
helm template vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace vm-feature-manager

# Install in dry-run mode
helm install vm-feature-manager ./deploy/helm/vm-feature-manager \
  --namespace vm-feature-manager \
  --dry-run --debug
```

## Uninstalling the Chart

### Standard Uninstall

```bash
helm uninstall vm-feature-manager --namespace vm-feature-manager
```

**Note:** This may leave behind some cluster-scoped resources (MutatingWebhookConfiguration, ClusterRole, ClusterRoleBinding).

### Complete Cleanup

For a complete cleanup including all "invisible" cluster-scoped resources, use the provided cleanup scripts:

```bash
# Standard cleanup (interactive, safe)
./deploy/helm/vm-feature-manager/cleanup.sh

# Force cleanup (use when standard cleanup fails)
./deploy/helm/vm-feature-manager/force-cleanup.sh
```

See [CLEANUP.md](./CLEANUP.md) for detailed documentation on cleanup scripts and troubleshooting.

### Quick Manual Cleanup

If you need to manually remove the webhook configuration (e.g., it's preventing cluster operations):

```bash
kubectl delete mutatingwebhookconfiguration vm-feature-manager
kubectl delete clusterrolebinding vm-feature-manager
kubectl delete clusterrole vm-feature-manager
kubectl delete namespace vm-feature-manager
```

## License

See the [LICENSE](../../../LICENSE) file for details.
