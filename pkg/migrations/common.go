package migrations

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func Run(migration string) error {
	if migration == "registry-data" {
		return registryData()
	}

	return fmt.Errorf("unknown migration: %s", migration)
}

// kubeClient returns a new kubernetes client.
func kubeClient() (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to process kubernetes config: %w", err)
	}
	return client.New(cfg, client.Options{})
}
