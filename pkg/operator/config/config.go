package config

// Config is configuration for the operator and should include things like
// operated images, release version, etc.
type Config struct {
	// OperatorReleaseVersion is the current version of operator.
	OperatorReleaseVersion string

	// Namespace is the operator namespace.
	Namespace string

	// ExternalDNSImage is the CoreDNS image to manage.
	ExternalDNSImage string
}
