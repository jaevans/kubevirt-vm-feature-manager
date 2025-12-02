# Copilot Instructions for kubevirt-vm-feature-manager

## Purpose & Shape
- Mutating admission webhook for KubeVirt VMs that turns simple annotations into VM spec mutations (nested virt, vBIOS injection via hook sidecar, PCI passthrough, GPU device plugin).
- Core flow: HTTP /mutate → decode AdmissionReview → build mutations via features → return JSONPatch replacing `/spec` and `/metadata/annotations`.
- Key packages: `pkg/webhook` (server, handler, mutator), `pkg/features` (feature implementations), `pkg/config` (env/flags), `pkg/utils` (constants/helpers).

## Where Things Live
- Entrypoint: `cmd/webhook/main.go` (config, k8s client, feature list, server start).
- Webhook: `pkg/webhook/{server.go,handler.go,mutator.go}`; health endpoints at `/healthz` and `/readyz` over TLS.
- Features: `pkg/features/*.go` implement `Feature` (Name, IsEnabled, Validate, Apply). Examples: `nested_virt.go`, `vbios_injection.go`, `pci_passthrough.go`, `gpu_device_plugin.go`.
- Conventions: Annotation namespace `vm-feature-manager.io/*`; tracking annotations use `*-applied`; constants in `pkg/utils/constants.go`.

## Build & Test
- Common targets (see `Makefile`):
  - `make build` (binary `./webhook`), `make test` (unit), `make test-integration` (envtest), `make test-all`, `make test-coverage`.
  - Tools: `make install-tools` and `make setup-envtest` (once) for integration tests.
- Run tests with verbose/watch: `make test-verbose`, `ginkgo watch -r --skip-package=test/integration`.

## Run & Debug Locally
- Needs TLS certs; server reads from `CERT_DIR` (default `/etc/webhook/certs`).
- Quick run:
  - `PORT=8443 CERT_DIR=./certs LOG_LEVEL=debug ./webhook`
- Flags (override env): `--port`, `--cert-dir`, `--error-handling`, `--log-level`.
- Logs via zap (controller-runtime); `debug` level logs feature detection details.

## Feature Pattern (add a new feature)
- Implement `features.Feature` with: `Name()`, `IsEnabled(vm)`, `Validate(ctx, vm, client)`, `Apply(ctx, vm, client)` returning `*features.MutationResult`.
- Define input/tracking annotation keys in `pkg/utils/constants.go`.
- Register in `cmd/webhook/main.go` by appending to `featureList` (order matters if features may interact).
- Examples to mirror:
  - Nested virt adds `CPU.Features` and initializes missing structs.
  - PCI/GPU validate input and error if `vm.Spec.Template` is nil (tests expect this).
  - vBIOS adds a `Volume` and a KubeVirt hook sidecar annotation on the VM template.

## Annotations & Error Modes
- Inputs: `nested-virt`, `vbios-injection`, `pci-passthrough` (JSON: `{ "devices": ["0000:00:02.0"] }`), `gpu-device-plugin`.
- Tracking: `*-applied` annotations added when `AddTrackingAnnotations=true` (default).
- Error handling (global): `ERROR_HANDLING_MODE=reject|allow-and-log|strip-label`.
  - `strip-label` removes the failing feature's input annotation and allows admission.
- vBIOS override sidecar image: `vm-feature-manager.io/sidecar-image`.
- **Userdata directives**: Features can be specified in cloud-init userdata using `x_kubevirt_features` YAML dictionary (e.g., `x_kubevirt_features: { nested_virt: enabled }`). Supports plain text, base64, and Secret references. Annotations take precedence over userdata directives.

## Helm & Deployment
- Chart: `deploy/helm/vm-feature-manager`. Values map to flags: `.webhook.port`, `.webhook.certDir`, `.errorHandling.mode`, `.logLevel`; extra env can be injected via `.Values.env`.
- TLS via cert-manager (default, CA injection on `MutatingWebhookConfiguration`) or manual `caBundle`.
- Mutating webhook path is `/mutate`; operations: CREATE/UPDATE on `kubevirt.io/v1` `VirtualMachine`.

## Gotchas & Tips
- Patch builder currently replaces `/spec` and `/metadata/annotations` wholesale; use fine-grained RFC6902 ops if you extend patching.
- Some features initialize missing structs (nested virt); others require an existing `spec.template` (PCI/GPU/vBIOS). Handle nils carefully to match tests.
- Config has per-feature toggles, but only nested virt reads `Enabled` directly; if you add toggles, wire them explicitly in feature code.
- Use `setup-envtest` + `make test-integration` for API-driven tests; unit tests use Ginkgo v2 + Gomega.

## Quick Examples
- Enable nested virt: add annotation `vm-feature-manager.io/nested-virt: "enabled"`.
- Add PCI passthrough: `vm-feature-manager.io/pci-passthrough: '{"devices":["0000:00:02.0"]}'`.
- Enable GPU plugin: `vm-feature-manager.io/gpu-device-plugin: "nvidia.com/gpu"`.
- vBIOS injection: `vm-feature-manager.io/vbios-injection: "<configmap>"` (+ optional `sidecar-image`).
- Userdata directives (for Rancher/Harvester):
  ```yaml
  #cloud-config
  x_kubevirt_features:
    nested_virt: enabled
    gpu_device_plugin: nvidia.com/gpu
  ```
