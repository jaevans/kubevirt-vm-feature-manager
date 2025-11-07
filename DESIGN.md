# KubeVirt VM Feature Manager - Design Document

## Overview

The KubeVirt VM Feature Manager is a **mutating admission webhook** for Harvester HCI that automatically modifies VirtualMachine objects based on feature annotations. This enables advanced capabilities like nested virtualization, vBIOS injection for iGPU passthrough, PCI device passthrough, and GPU device plugin configuration.

**Target Environment**: Harvester HCI (built on Rancher + KubeVirt)  
**Primary Language**: Go 1.25  
**Development Approach**: Test-Driven Development (TDD) with Ginkgo v2 + Gomega

## Build & Release Standards

### Zero-Deprecation Policy

**Principle**: Net-new work must not introduce deprecated configurations or patterns.

**Rationale**:
- Deprecated features signal technical debt from day one
- Modern tooling offers better alternatives
- Reduces future maintenance burden
- Demonstrates commitment to current best practices

**Application**:
- Build configurations (GoReleaser, Docker, etc.) must use current, non-deprecated syntax
- Dependencies should be on supported versions
- CI/CD pipelines use actively maintained actions/tools
- When replacing tools, migrate to modern equivalents, not deprecated legacy patterns

**Exceptions**:
- External dependencies that haven't yet provided migration paths (document as known issue)
- Explicitly documented temporary workarounds with removal timeline

## Architecture

### Webhook Type: Mutating Admission Webhook

**Decision**: Mutating webhook over validating webhook
- **Rationale**: Need to modify VM specs (add CPU features, inject hooks, add device plugins), not just validate
- **Future**: May add optional validating webhook later for enhanced validation

### Annotation vs Labels

**Decision**: Use **annotations** instead of labels
- **Rationale**:
  - Labels have character restrictions (63 chars, limited charset)
  - Annotations support larger values and arbitrary JSON
  - ConfigMap names, version strings, and complex configuration fit better in annotations
  - Labels are for selection/grouping; annotations are for metadata/configuration

### Annotation Namespace

**Decision**: `vm-feature-manager.io/*`
- **Input annotations**: `vm-feature-manager.io/<feature-name>` (user-specified)
- **Output annotations**: `vm-feature-manager.io/<feature-name>-applied` (tracking/status)

### Deployment Model

- **Primary**: Helm chart with deployment in dedicated namespace
- **Components**:
  - Webhook server deployment (HTTPS with TLS)
  - MutatingWebhookConfiguration
  - Service (for webhook endpoint)
  - TLS certificates (cert-manager or manual)
  - RBAC (ServiceAccount, ClusterRole, ClusterRoleBinding)

## Features

### 1. Nested Virtualization

**Annotation**: `vm-feature-manager.io/nested-virt: "enabled"`

**Purpose**: Enable CPU nested virtualization for running VMs inside VMs

**Implementation**:
- Detects host CPU vendor (AMD vs Intel)
- AMD: Adds `svm` to `spec.template.spec.domain.cpu.features[]`
- Intel: Adds `vmx` to `spec.template.spec.domain.cpu.features[]`
- Ensures `policy: require` for each feature
- Prevents duplicates if feature already exists

**Status Tracking**: 
- On success: `vm-feature-manager.io/nested-virt-applied: "true"`
- On error: `vm-feature-manager.io/nested-virt-error: "<error message>"`

**Configuration**:
```yaml
nestedVirtualization:
  enabled: true  # ENV: NESTED_VIRT_ENABLED (default: true)
  errorHandling: "reject"  # ENV: NESTED_VIRT_ERROR_HANDLING
```

**Status**: ✅ Fully implemented and tested (15 tests)

### 2. vBIOS Injection (KubeVirt Hook Sidecar)

**Annotation**: `vm-feature-manager.io/vbios-injection: "<configmap-name>"`

**Purpose**: Inject vBIOS ROM blob to enable iGPU passthrough (AMD iGPU specific use case)

**Implementation Details**:

#### Why Hook Sidecar Pattern?
After extensive research, the **KubeVirt sidecar hook** is the **ONLY** working solution for vBIOS injection:
- **NOT** possible via cloud-init (runs too late, inside guest)
- **NOT** possible via domain XML mutation alone (libvirt validates ROM paths)
- **NOT** possible via init containers (no access to VM pod filesystem)
- **ONLY** solution: KubeVirt's `sidecar-shim-image` with `onDefineDomain` hook

#### Hook Implementation
```yaml
spec:
  template:
    metadata:
      annotations:
        hooks.kubevirt.io/hookSidecars: >
          [
            {
              "image": "registry.example.com/kubevirt/sidecar-shim:v1.4.0",
              "imagePullPolicy": "IfNotPresent",
              "args": [
                "--version", "v1alpha2",
                "--hook-type", "onDefineDomain"
              ]
            }
          ]
    spec:
      volumes:
        - name: vbios-rom
          configMap:
            name: <configmap-name>  # from annotation value
      domain:
        devices:
          hostDevices:
            - name: igpu
              deviceName: <pci-address>
```

#### ConfigMap Structure
The ConfigMap must contain the vBIOS ROM as binary data:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: amd-igpu-vbios
  namespace: <vm-namespace>
binaryData:
  rom: <base64-encoded-vbios-blob>  # Key MUST be "rom"
```

#### Hook Script Logic
The sidecar hook receives the domain XML and modifies it:
1. Locate the PCI device in domain XML
2. Add ROM file reference pointing to mounted ConfigMap volume
3. Return modified domain XML to KubeVirt

#### KubeVirt Version Detection
- **Auto-detect**: Query KubeVirt API for version, select matching sidecar image
- **Override**: `vm-feature-manager.io/sidecar-image: "custom-registry/sidecar:v1.4.0"`
- **Default**: `registry.k8s.io/kubevirt/sidecar-shim:v1.4.0`

#### Limitations
- **One vBIOS per VM**: Multiple vBIOS blobs not supported (use case doesn't require it)
- **AMD iGPU specific**: Designed for AMD integrated GPU passthrough
- **ConfigMap in same namespace**: Security boundary (no cross-namespace ConfigMap access)

**Status Tracking**:
- Success: `vm-feature-manager.io/vbios-injection-applied: "<configmap-name>"`
- Error: `vm-feature-manager.io/vbios-injection-error: "<error message>"`

**Configuration**:
```yaml
vbiosInjection:
  enabled: true  # ENV: VBIOS_INJECTION_ENABLED (default: true)
  errorHandling: "reject"  # ENV: VBIOS_INJECTION_ERROR_HANDLING
  defaultSidecarImage: "registry.k8s.io/kubevirt/sidecar-shim:v1.4.0"
  autoDetectVersion: true  # Auto-select sidecar image based on KubeVirt version
```

**Status**: ✅ Fully implemented and tested (21 tests)

### 3. PCI Passthrough

**Annotation**: `vm-feature-manager.io/pci-passthrough: '{"devices": ["0000:00:02.0"]}'`

**Purpose**: Enable PCI device passthrough to VM

**Implementation**:
- Parse JSON array of PCI addresses from annotation value
- Add each device to `spec.template.spec.domain.devices.hostDevices[]`
- Validate PCI address format (BDF notation: `DDDD:BB:DD.F`)
- Check for duplicate device assignments

**Host Device Structure**:
```yaml
spec:
  template:
    spec:
      domain:
        devices:
          hostDevices:
            - name: device-0  # auto-generated name
              deviceName: pci_0000_00_02_0  # sanitized PCI address
```

**Status Tracking**:
- Success: `vm-feature-manager.io/pci-passthrough-applied: '["0000:00:02.0"]'`
- Error: `vm-feature-manager.io/pci-passthrough-error: "<error message>"`

**Configuration**:
```yaml
pciPassthrough:
  enabled: true  # ENV: PCI_PASSTHROUGH_ENABLED (default: true)
  errorHandling: "reject"  # ENV: PCI_PASSTHROUGH_ERROR_HANDLING
  maxDevices: 8  # Maximum PCI devices per VM
```

**Status**: ✅ Fully implemented and tested (19 tests)

### 4. GPU Device Plugin

**Annotation**: `vm-feature-manager.io/gpu-device-plugin: "nvidia.com/gpu"`

**Purpose**: Enable GPU scheduling via Kubernetes device plugins

**Implementation**:
- Add resource limit to pod spec: `resources.limits[<plugin-name>]: "1"`
- Common values:
  - `nvidia.com/gpu` - NVIDIA GPU operator
  - `amd.com/gpu` - AMD GPU operator
  - `intel.com/gpu` - Intel GPU operator
- Validates device plugin name format (DNS subdomain)

**Pod Spec Modification**:
```yaml
spec:
  template:
    spec:
      domain:
        resources:
          limits:
            nvidia.com/gpu: "1"
```

**Status Tracking**:
- Success: `vm-feature-manager.io/gpu-device-plugin-applied: "nvidia.com/gpu"`
- Error: `vm-feature-manager.io/gpu-device-plugin-error: "<error message>"`

**Configuration**:
```yaml
gpuDevicePlugin:
  enabled: true  # ENV: GPU_DEVICE_PLUGIN_ENABLED (default: true)
  errorHandling: "reject"  # ENV: GPU_DEVICE_PLUGIN_ERROR_HANDLING
  allowedPlugins:  # Whitelist of allowed device plugin names
    - "nvidia.com/gpu"
    - "amd.com/gpu"
    - "intel.com/gpu"
```

**Status**: ✅ Fully implemented and tested (17 tests)

## Error Handling Strategy

### Error Handling Modes

Each feature supports three error handling modes (configurable via environment variables):

1. **`reject`** (default): Reject the admission request with error message
   - Use case: Strict validation, prevent misconfiguration
   - Response: HTTP 200 with `allowed: false` and status message

2. **`allow-and-log`**: Allow admission, log error, add error annotation
   - Use case: Non-critical features, prefer VM creation over feature enforcement
   - Response: HTTP 200 with `allowed: true`, error logged and annotated

3. **`strip-label`**: Remove the feature annotation and allow admission
   - Use case: Graceful degradation, silently ignore unsupported features
   - Response: HTTP 200 with `allowed: true`, annotation removed from response

### Error Annotation Format

When a feature fails, an error annotation is added:
```yaml
metadata:
  annotations:
    vm-feature-manager.io/<feature>-error: "<error message>"
```

### Testing Error Handling

**Requirement**: "Error handling is something that should be tested" (user requirement)

Each feature's test suite includes:
- Tests for each error handling mode
- Validation of error messages
- Verification of error annotations
- Proper HTTP response codes and AdmissionReview responses

## Configuration System

### Environment Variables

All configuration loaded from environment variables (12-factor app):

```bash
# Server Configuration
PORT=8443                          # HTTPS port
TLS_CERT_FILE=/etc/certs/tls.crt  # TLS certificate path
TLS_KEY_FILE=/etc/certs/tls.key   # TLS private key path

# Feature Toggles
NESTED_VIRT_ENABLED=true
VBIOS_INJECTION_ENABLED=true
PCI_PASSTHROUGH_ENABLED=true
GPU_DEVICE_PLUGIN_ENABLED=true

# Error Handling (per feature)
NESTED_VIRT_ERROR_HANDLING=reject
VBIOS_INJECTION_ERROR_HANDLING=reject
PCI_PASSTHROUGH_ERROR_HANDLING=reject
GPU_DEVICE_PLUGIN_ERROR_HANDLING=reject

# vBIOS Injection Specific
VBIOS_DEFAULT_SIDECAR_IMAGE=registry.k8s.io/kubevirt/sidecar-shim:v1.4.0
VBIOS_AUTO_DETECT_VERSION=true

# PCI Passthrough Specific
PCI_MAX_DEVICES=8

# Tracking Annotations
ADD_TRACKING_ANNOTATIONS=true  # Add *-applied annotations on success
```

### Configuration Structure

```go
type Config struct {
    Port                     int
    TLSCertFile              string
    TLSKeyFile               string
    AddTrackingAnnotations   bool
    NestedVirtualization     NestedVirtConfig
    VBiosInjection           VBiosInjectionConfig
    PciPassthrough           PciPassthroughConfig
    GpuDevicePlugin          GpuDevicePluginConfig
}

type NestedVirtConfig struct {
    Enabled        bool
    ErrorHandling  string // "reject", "allow-and-log", "strip-label"
}

// Similar structures for other features
```

## Testing Strategy

### Test Framework

- **Framework**: Ginkgo v2 (BDD-style testing)
- **Assertions**: Gomega (matcher library)
- **Mocking**: mockery v2 (for controller-runtime client mocks)
- **Integration**: envtest (Kubernetes API server for integration tests)

### Test Organization

```
pkg/
├── config/
│   ├── config.go
│   ├── config_test.go        # 12 tests - env var parsing, defaults
│   └── config_suite_test.go
├── features/
│   ├── feature.go
│   ├── nested_virt.go
│   ├── nested_virt_test.go   # 15 tests - all feature methods
│   ├── vbios_injection.go
│   ├── vbios_injection_test.go
│   ├── pci_passthrough.go
│   ├── pci_passthrough_test.go
│   ├── gpu_device_plugin.go
│   ├── gpu_device_plugin_test.go
│   └── features_suite_test.go
└── webhook/
    ├── mutator.go
    ├── mutator_test.go
    ├── handler.go
    ├── handler_test.go
    └── webhook_suite_test.go
```

### Test Coverage Requirements

Each feature must have tests for:
1. **Name()**: Returns correct feature name
2. **IsEnabled()**: Checks annotation presence and value parsing
3. **Validate()**: 
   - Valid inputs
   - Invalid inputs (malformed JSON, bad PCI addresses, etc.)
   - Missing ConfigMaps (for vBIOS injection)
   - Duplicate devices
4. **Apply()**:
   - Success path with mutation
   - Nil template/spec handling
   - Existing configuration (no duplicates)
   - Error handling for each mode (reject, allow-and-log, strip-label)
   - Annotation tracking (success and error annotations)

### Current Test Status

- ✅ Config tests: 12/12 passing
- ✅ Feature tests: 76/76 passing
  - Nested Virt: 15 tests
  - PCI Passthrough: 19 tests
  - vBIOS Injection: 21 tests
  - GPU Device Plugin: 21 tests
- **Total Unit Tests: 88/88 passing (100%)**
- ✅ Integration tests: 11/11 passing (envtest)
  - Nested Virtualization (1 test)
  - PCI Passthrough (2 tests)
  - vBIOS Injection (2 tests)
  - GPU Device Plugin (3 tests)
  - Combined features (1 test)
  - Error handling (2 tests)
- **Grand Total: 99 tests, all passing**

### Bug Found via TDD

During nested virtualization implementation, TDD caught a validation bug:
- **Issue**: `Validate()` was checking annotation value without first checking if annotation exists
- **Impact**: Would panic on nil map access
- **Fix**: Added existence check before value validation
- **Lesson**: TDD caught a real bug that would have caused runtime panics

## Feature Interface

### Design Pattern

All features implement a common interface for consistency and extensibility:

```go
type Feature interface {
    // Name returns the feature name (e.g., "nested-virt")
    Name() string
    
    // IsEnabled checks if the feature is requested via annotation
    IsEnabled(vm *kubevirtv1.VirtualMachine) bool
    
    // Validate performs feature-specific validation
    // Returns error if configuration is invalid
    Validate(ctx context.Context, vm *kubevirtv1.VirtualMachine, client client.Client) error
    
    // Apply modifies the VM spec to enable the feature
    // Returns MutationResult with changes and annotations
    Apply(ctx context.Context, vm *kubevirtv1.VirtualMachine, cfg interface{}) MutationResult
}

type MutationResult struct {
    Modified           bool              // Whether VM was modified
    AnnotationsToAdd   map[string]string // Tracking annotations to add
    ErrorMessage       string            // Error message if failed
}
```

### Why This Interface?

- **Consistency**: All features follow the same lifecycle (check → validate → apply)
- **Testability**: Easy to mock and test individual features
- **Extensibility**: New features just implement the interface
- **Separation of Concerns**: Webhook handler doesn't know feature details
- **Error Handling**: Standardized error reporting via MutationResult

## Webhook Flow

### Request Processing

```
1. HTTP Request → Handler (ServeHTTP)
   ↓
2. Decode AdmissionReview from request body
   ↓
3. Extract VirtualMachine from AdmissionRequest
   ↓
4. Mutator.Handle(vm, config, features)
   ↓
5. For each enabled feature:
   a. feature.IsEnabled(vm) → skip if false
   b. feature.Validate(vm) → handle error per mode
   c. feature.Apply(vm) → collect mutations
   ↓
6. Create JSON Patch from mutations
   ↓
7. Build AdmissionResponse:
   - allowed: true (or false if error mode = reject)
   - patch: base64(json-patch)
   - patchType: JSONPatch
   ↓
8. Encode AdmissionReview response
   ↓
9. HTTP 200 with JSON response
```

### JSON Patch Generation

Mutations are converted to RFC 6902 JSON Patch operations:

```json
[
  {
    "op": "add",
    "path": "/spec/template/spec/domain/cpu/features/-",
    "value": {
      "name": "svm",
      "policy": "require"
    }
  },
  {
    "op": "add",
    "path": "/metadata/annotations/vm-feature-manager.io~1nested-virt-applied",
    "value": "true"
  }
]
```

**Note**: Annotation keys with `/` are escaped as `~1` per RFC 6901 (JSON Pointer)

## Security Considerations

### RBAC Permissions

The webhook ServiceAccount needs:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: vm-feature-manager
rules:
  # Read VirtualMachines for admission
  - apiGroups: ["kubevirt.io"]
    resources: ["virtualmachines"]
    verbs: ["get", "list"]
  
  # Read ConfigMaps for vBIOS injection validation
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]
  
  # Read KubeVirt resource for version detection
  - apiGroups: ["kubevirt.io"]
    resources: ["kubevirts"]
    verbs: ["get"]
```

### ConfigMap Access

- **Scope**: vBIOS ConfigMaps must be in the **same namespace** as the VM
- **Validation**: Webhook validates ConfigMap exists and has `binaryData.rom` key
- **Security**: No cross-namespace access (prevents privilege escalation)

### TLS Configuration

- **Requirement**: Webhook must use HTTPS with valid TLS certificate
- **Certificate Sources**:
  1. cert-manager (automated, recommended)
  2. Manual certificate creation
  3. Kubernetes CA (for internal-only deployments)
- **Verification**: `caBundle` in MutatingWebhookConfiguration must match server cert

### Admission Webhook Security

- **Fail-safe**: `failurePolicy: Fail` (reject on webhook unavailability)
- **Timeout**: 10s timeout to prevent hanging admissions
- **Reinvocation**: `reinvocationPolicy: IfNeeded` (allow other webhooks to run first)
- **Scope**: Only mutate VirtualMachine resources in configured namespaces

## Code Quality Standards

### Linting

- **Tool**: golangci-lint v2.6.1
- **Enabled Linters**:
  - errcheck (error checking)
  - govet (suspicious code)
  - ineffassign (ineffectual assignments)
  - staticcheck (static analysis)
  - unused (unused code)
  - misspell (spelling)
  - revive (code style)
  - bodyclose (HTTP body closing)
  - ginkgolinter (Ginkgo best practices)
- **Formatters**: gofmt, goimports
- **Current Status**: 0 issues

### Documentation

- **Package comments**: Required for all packages
- **Exported symbols**: All exported constants, types, and functions documented
- **Examples**: Example VM manifests in `examples/` directory

### Test Requirements

- **Coverage**: Aim for >80% test coverage
- **Error paths**: All error handling modes must be tested
- **Edge cases**: Nil checks, duplicate prevention, invalid input
- **Integration**: envtest for end-to-end webhook testing

## Build and Deployment

### Dockerfile

Multi-stage build for minimal image size:

```dockerfile
# Stage 1: Build
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o webhook ./cmd/webhook

# Stage 2: Runtime
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /build/webhook /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/webhook"]
```

### Makefile Targets

```makefile
build:      # Build binary
test:       # Run all tests with Ginkgo
lint:       # Run golangci-lint
lint-fix:   # Auto-fix linting issues
mocks:      # Generate mocks with mockery
clean:      # Clean build artifacts
docker:     # Build Docker image
```

### Helm Chart Structure

```
deploy/helm/vm-feature-manager/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── mutatingwebhook.yaml
│   ├── serviceaccount.yaml
│   ├── clusterrole.yaml
│   ├── clusterrolebinding.yaml
│   └── certificate.yaml  # cert-manager Certificate resource
```

### Helm Values

```yaml
image:
  repository: ghcr.io/jaevans/vm-feature-manager
  tag: v1.0.0
  pullPolicy: IfNotPresent

replicas: 2  # HA deployment

resources:
  limits:
    cpu: 200m
    memory: 128Mi
  requests:
    cpu: 100m
    memory: 64Mi

features:
  nestedVirtualization:
    enabled: true
    errorHandling: reject
  
  vbiosInjection:
    enabled: true
    errorHandling: reject
    defaultSidecarImage: registry.k8s.io/kubevirt/sidecar-shim:v1.4.0
    autoDetectVersion: true
  
  pciPassthrough:
    enabled: true
    errorHandling: reject
    maxDevices: 8
  
  gpuDevicePlugin:
    enabled: true
    errorHandling: reject
    allowedPlugins:
      - nvidia.com/gpu
      - amd.com/gpu
      - intel.com/gpu

tls:
  certManager:
    enabled: true
    issuer: selfsigned-issuer
  # Or manual:
  # certFile: /path/to/tls.crt
  # keyFile: /path/to/tls.key

webhook:
  failurePolicy: Fail
  timeout: 10
  namespaceSelector:
    matchLabels:
      vm-features: enabled
```

## Development Workflow

### Initial Setup

```bash
# Clone repository
git clone https://github.com/jaevans/kubevirt-vm-feature-manager.git
cd kubevirt-vm-feature-manager

# Install dependencies
go mod download

# Install development tools
go install github.com/onsi/ginkgo/v2/ginkgo@latest
go install github.com/vektra/mockery/v2@latest
```

### TDD Cycle

1. **Write test** for new feature or functionality
2. **Run tests** - watch it fail: `make test`
3. **Implement** minimum code to pass test
4. **Run tests** again - watch it pass: `make test`
5. **Refactor** code while keeping tests green
6. **Run lint** - ensure code quality: `make lint`
7. **Commit** with descriptive message

### Running Locally

```bash
# Run tests
make test

# Run with coverage
ginkgo -r --cover --coverprofile=coverage.out
go tool cover -html=coverage.out

# Run linter
make lint

# Build binary
make build

# Run webhook (requires kubeconfig)
export KUBECONFIG=~/.kube/config
export TLS_CERT_FILE=./certs/tls.crt
export TLS_KEY_FILE=./certs/tls.key
./bin/webhook
```

## Future Enhancements

### Potential Features

1. **USB Passthrough**: Similar to PCI, but for USB devices
2. **NUMA Configuration**: Optimize for NUMA topologies
3. **CPU Pinning**: Pin vCPUs to physical cores
4. **Hugepages**: Configure hugepage memory
5. **SRIOV**: Enable SR-IOV network devices
6. **VFIO**: Additional VFIO device configurations

### Validating Webhook

Optional validating webhook to enforce policies:
- Validate PCI device availability on nodes
- Check ConfigMap permissions before vBIOS injection
- Enforce organizational policies (e.g., max GPUs per VM)
- Quota management

### Observability

1. **Metrics** (Prometheus):
   - `webhook_requests_total{feature, result}`
   - `webhook_duration_seconds{feature}`
   - `feature_errors_total{feature, error_mode}`

2. **Tracing** (OpenTelemetry):
   - Trace entire admission request lifecycle
   - Feature validation and application spans

3. **Structured Logging**:
   - JSON logs with request IDs
   - Feature-specific log levels
   - Audit logging for compliance

## Technical Decisions Log

### Why KubeVirt Hook Sidecar for vBIOS?

**Decision**: Use KubeVirt sidecar-shim with onDefineDomain hook

**Alternatives Considered**:
1. ❌ Cloud-init: Runs inside guest, too late for vBIOS
2. ❌ Direct domain XML: No way to inject ROM without hook
3. ❌ Init container: No access to VM pod after start
4. ✅ Hook sidecar: Only working solution

**Trade-offs**:
- ✅ Pros: Works reliably, supported by KubeVirt upstream
- ❌ Cons: Requires additional container, version coupling

### Why Annotations over Labels?

**Decision**: Use annotations for feature configuration

**Rationale**:
- Labels limited to 63 chars, restricted charset
- ConfigMap names can exceed label limits
- JSON configuration fits naturally in annotations
- Labels are for selection, annotations for metadata

### Why Go over Python/Shell?

**Decision**: Implement in Go

**Rationale**:
- KubeVirt ecosystem is Go-based
- Type safety for Kubernetes API interactions
- Better performance than Python
- controller-runtime provides excellent libraries
- Native Kubernetes tooling (client-go, envtest)

## Constants Reference

All constants defined in `pkg/utils/constants.go`:

### Feature Names
- `FeatureNestedVirt = "nested-virt"`
- `FeatureVBiosInjection = "vbios-injection"`
- `FeaturePciPassthrough = "pci-passthrough"`
- `FeatureGpuDevicePlugin = "gpu-device-plugin"`

### CPU Features
- `CPUFeatureSVM = "svm"` (AMD nested virt)
- `CPUFeatureVMX = "vmx"` (Intel nested virt)

### Annotation Keys (Input)
- `AnnotationNestedVirt = "vm-feature-manager.io/nested-virt"`
- `AnnotationVBiosInjection = "vm-feature-manager.io/vbios-injection"`
- `AnnotationPciPassthrough = "vm-feature-manager.io/pci-passthrough"`
- `AnnotationGpuDevicePlugin = "vm-feature-manager.io/gpu-device-plugin"`
- `AnnotationSidecarImage = "vm-feature-manager.io/sidecar-image"`

### Annotation Keys (Output - Tracking)
- `AnnotationNestedVirtApplied = "vm-feature-manager.io/nested-virt-applied"`
- `AnnotationVBiosInjectionApplied = "vm-feature-manager.io/vbios-injection-applied"`
- `AnnotationPciPassthroughApplied = "vm-feature-manager.io/pci-passthrough-applied"`
- `AnnotationGpuDevicePluginApplied = "vm-feature-manager.io/gpu-device-plugin-applied"`

### Annotation Keys (Output - Errors)
- `AnnotationNestedVirtError = "vm-feature-manager.io/nested-virt-error"`
- `AnnotationVBiosInjectionError = "vm-feature-manager.io/vbios-injection-error"`
- `AnnotationPciPassthroughError = "vm-feature-manager.io/pci-passthrough-error"`
- `AnnotationGpuDevicePluginError = "vm-feature-manager.io/gpu-device-plugin-error"`

### Hook Sidecar
- `HookAnnotationKey = "hooks.kubevirt.io/hookSidecars"`
- `DefaultSidecarImage = "registry.k8s.io/kubevirt/sidecar-shim:v1.4.0"`
- `SidecarHookVersion = "v1alpha2"`
- `SidecarHookType = "onDefineDomain"`

### ConfigMap
- `VBiosConfigMapKey = "rom"` (key in ConfigMap binaryData)

### Error Handling Modes
- `ErrorHandlingReject = "reject"`
- `ErrorHandlingAllowAndLog = "allow-and-log"`
- `ErrorHandlingStripLabel = "strip-label"`

### Defaults
- `DefaultPort = 8443`
- `DefaultTLSCertFile = "/etc/webhook/certs/tls.crt"`
- `DefaultTLSKeyFile = "/etc/webhook/certs/tls.key"`

## References

### Documentation
- [KubeVirt Documentation](https://kubevirt.io/user-guide/)
- [KubeVirt Hooks](https://kubevirt.io/user-guide/virtual_machines/hooks/)
- [Kubernetes Admission Webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [RFC 6902 JSON Patch](https://tools.ietf.org/html/rfc6902)
- [RFC 6901 JSON Pointer](https://tools.ietf.org/html/rfc6901)

### Dependencies
- KubeVirt v1.3.1
- controller-runtime v0.22.4
- Ginkgo v2
- Gomega v1.36.2
- mockery v2

## Changelog

### v0.1.0 (In Progress)

**Completed**:
- ✅ Project structure and scaffolding
- ✅ Configuration system with environment variables
- ✅ Feature interface design
- ✅ Nested virtualization feature (fully tested - 15 tests)
- ✅ PCI passthrough feature (fully tested - 19 tests)
- ✅ vBIOS injection feature (fully tested - 21 tests)
- ✅ GPU device plugin feature (fully tested - 17 tests)
- ✅ Webhook server with TLS and health endpoints
- ✅ Admission webhook handler and mutator
- ✅ Test framework setup (Ginkgo v2, Gomega)
- ✅ Code quality tools (golangci-lint, mockery)
- ✅ **All 4 features implemented - 84 tests passing, 0 lint issues**

**Planned**:
- 📋 Helm chart deployment
- 📋 Integration tests with envtest
- 📋 CI/CD pipeline
- 📋 Documentation and examples

---

**Last Updated**: November 6, 2025  
**Status**: Active Development - All Features Implemented
**Next Milestone**: Complete all four features with TDD
