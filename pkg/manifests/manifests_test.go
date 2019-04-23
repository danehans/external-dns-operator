package manifests

import (
	"testing"
)

func TestManifests(t *testing.T) {
	ExternalDNSServiceAccount()
	ExternalDNSClusterRole()
	ExternalDNSClusterRoleBinding()
	ExternalDNSNamespace()
	ExternalDNSDeployment()
}
