package migrations

import "fmt"

func Run(migration string) error {
	if migration == "registry-data" {
		return registryData()
	}
	if migration == "registry-scale" {
		return registryScale(0)
	}

	return fmt.Errorf("unknown migration: %s", migration)
}
