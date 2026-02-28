package engine

// Provider identifies the resource management backend.
type Provider string

const (
	ProviderLocal      Provider = "local"
	ProviderKubernetes Provider = "kubernetes"
)
