package main_test

import (
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// Access the scheme from main package
var scheme *runtime.Scheme

func init() {
	// Initialize the scheme the same way main does
	scheme = runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = kubevirtv1.AddToScheme(scheme)
}

var _ = Describe("Scheme Registration", func() {
	Describe("Required Types", func() {
		// TestSchemeRegistration validates that all required Kubernetes types
		// are registered in the global scheme. This test prevents runtime errors
		// like "no kind is registered for the type v1.Secret in scheme" that occur
		// when the client tries to work with unregistered types.
		DescribeTable("should register type",
			func(gvk schema.GroupVersionKind, expected runtime.Object) {
				// Attempt to create a new instance of the type using the scheme
				obj, err := scheme.New(gvk)
				Expect(err).NotTo(HaveOccurred(), "Failed to create object from GVK %v. This indicates the type is not registered in the scheme. Add the missing AddToScheme() call in init().", gvk)
				Expect(obj).NotTo(BeNil(), "scheme.New() returned nil for GVK %v", gvk)

				// Check that the type matches what we expect
				expectedType := reflect.TypeOf(expected)
				actualType := reflect.TypeOf(obj)
				Expect(actualType).To(Equal(expectedType), "Type mismatch for GVK %v", gvk)
			},
			Entry("corev1.Secret", schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Secret",
			}, &corev1.Secret{}),
			Entry("corev1.ConfigMap", schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			}, &corev1.ConfigMap{}),
			Entry("corev1.Pod", schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			}, &corev1.Pod{}),
			Entry("kubevirt.VirtualMachine", schema.GroupVersionKind{
				Group:   "kubevirt.io",
				Version: "v1",
				Kind:    "VirtualMachine",
			}, &kubevirtv1.VirtualMachine{}),
			Entry("kubevirt.VirtualMachineInstance", schema.GroupVersionKind{
				Group:   "kubevirt.io",
				Version: "v1",
				Kind:    "VirtualMachineInstance",
			}, &kubevirtv1.VirtualMachineInstance{}),
		)
	})

	Describe("Known Types", func() {
		// TestSchemeKnownTypes validates that all types we need are known to the scheme
		// by checking the scheme's KnownTypes map directly.
		DescribeTable("should be in scheme's KnownTypes",
			func(gv schema.GroupVersion, kind string) {
				knownTypes := scheme.KnownTypes(gv)
				Expect(knownTypes).NotTo(BeNil(), "No types registered for GroupVersion %v", gv)
				Expect(knownTypes).To(HaveKey(kind), "Type %s not found in scheme for GroupVersion %v", kind, gv)
			},
			Entry("v1/Secret", corev1.SchemeGroupVersion, "Secret"),
			Entry("v1/ConfigMap", corev1.SchemeGroupVersion, "ConfigMap"),
			Entry("v1/Pod", corev1.SchemeGroupVersion, "Pod"),
			Entry("kubevirt.io/v1/VirtualMachine", kubevirtv1.SchemeGroupVersion, "VirtualMachine"),
			Entry("kubevirt.io/v1/VirtualMachineInstance", kubevirtv1.SchemeGroupVersion, "VirtualMachineInstance"),
		)
	})
})
