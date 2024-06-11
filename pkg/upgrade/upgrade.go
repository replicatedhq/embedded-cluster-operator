package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	jsonpatch "github.com/evanphx/json-patch"
	helmv1beta1 "github.com/k0sproject/k0s/pkg/apis/helm/v1beta1"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	chartName      = "embedded-cluster-operator"
	chartNamespace = "kube-system"
)

func Upgrade(ctx context.Context, cli client.Client) error {
	in, err := getCurrentInstallation(ctx, cli)
	if err != nil {
		return fmt.Errorf("get current installation: %w", err)
	}

	metadata, err := release.MetadataFor(ctx, in, cli)
	if err != nil {
		return fmt.Errorf("get release metadata: %w", err)
	}

	existing, err := getExistingOperatorChart(ctx, cli)
	if err != nil {
		return fmt.Errorf("get existing operator chart: %w", err)
	}

	chart, err := getOperatorChartFromMetadata(metadata)
	if err != nil {
		return fmt.Errorf("get operator chart from metadata: %w", err)
	}

	err = patchOperatorChart(ctx, cli, existing, chart)
	if err != nil {
		return fmt.Errorf("patch operator chart: %w", err)
	}

	return nil
}

func patchOperatorChart(ctx context.Context, cli client.Client, existing *helmv1beta1.Chart, chart *k0sv1beta1.Chart) error {
	original, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal existing chart: %w", err)
	}

	desired := existing.DeepCopy()
	desired.Spec.ChartName = chart.ChartName
	desired.Spec.Version = chart.Version
	desired.Spec.Values = chart.Values
	desired.Spec.Namespace = chart.TargetNS
	desired.Spec.Timeout = chart.Timeout.String()
	desired.Spec.Order = chart.Order

	modified, err := json.Marshal(desired)
	if err != nil {
		return fmt.Errorf("marshal desired chart: %w", err)
	}

	patchData, err := jsonpatch.CreateMergePatch(original, modified)
	if err != nil {
		return fmt.Errorf("create json merge patch: %w", err)
	}
	patch := client.RawPatch(types.MergePatchType, patchData)
	err = cli.Patch(ctx, existing, patch)
	if err != nil {
		return fmt.Errorf("patch chart: %w", err)
	}

	return nil
}

func getExistingOperatorChart(ctx context.Context, cli client.Client) (*helmv1beta1.Chart, error) {
	chart := &helmv1beta1.Chart{}
	name := fmt.Sprintf("k0s-addon-chart-%s", chartName)
	err := cli.Get(ctx, client.ObjectKey{Name: name, Namespace: chartNamespace}, chart)
	if err != nil {
		return nil, fmt.Errorf("get chart: %w", err)
	}
	return chart, nil
}

func getOperatorChartFromMetadata(metadata *ectypes.ReleaseMetadata) (*k0sv1beta1.Chart, error) {
	for _, chart := range metadata.Configs.Charts {
		if chart.Name == chartName {
			return &chart, nil
		}
	}
	return nil, fmt.Errorf("chart not found")
}

func getCurrentInstallation(ctx context.Context, cli client.Client) (*v1beta1.Installation, error) {
	var installs clusterv1beta1.InstallationList
	if err := cli.List(ctx, &installs); err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	items := installs.Items
	sort.SliceStable(items, func(i, j int) bool {
		return items[j].Name < items[i].Name
	})
	for _, in := range installs.Items {
		if in.Status.State != v1beta1.InstallationStateObsolete {
			return &in, nil
		}
	}
	return nil, fmt.Errorf("no active installations found")
}
