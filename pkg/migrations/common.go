package migrations

import (
	"context"
	"fmt"
)

func Run(ctx context.Context, migration string) error {
	if migration == "registry-data" {
		return registryData(ctx)
	}

	return fmt.Errorf("unknown migration: %s", migration)
}
