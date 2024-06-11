package k8sutil

import (
	"context"
	"fmt"
	"sort"

	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetCurrentInstallation(ctx context.Context, cli client.Client) (*v1beta1.Installation, error) {
	var installs v1beta1.InstallationList
	if err := cli.List(ctx, &installs); err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	items := installs.Items
	sort.SliceStable(items, func(i, j int) bool {
		return items[j].Name < items[i].Name
	})
	return &items[0], nil
}
