package userdata_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserdata(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Userdata Suite")
}
