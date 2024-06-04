package rqlite

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func IsRqliteReady(ctx context.Context, cli client.Client) (bool, error) {
	// check if the rqlite statefulset has three healthy pods
	registry := appsv1.StatefulSet{}
	if err := cli.Get(ctx, client.ObjectKey{Namespace: "kotsadm", Name: "kotsadm-rqlite"}, &registry); err != nil {
		return false, fmt.Errorf("get registry: %w", err)
	}

	if registry.Status.ReadyReplicas != 3 {
		return false, nil
	}

	return true, nil
}
