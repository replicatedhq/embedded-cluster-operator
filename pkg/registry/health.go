package registry

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func IsRegistryReady(ctx context.Context, cli client.Client) (bool, error) {
	// check if the registry deployment has two healthy pods
	registry := appsv1.Deployment{}
	if err := cli.Get(ctx, client.ObjectKey{Namespace: registryNamespace, Name: "registry"}, &registry); err != nil {
		return false, fmt.Errorf("get registry: %w", err)
	}

	if registry.Status.ReadyReplicas != 2 {
		return false, nil
	}

	return true, nil
}
