package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	jsonpatch "github.com/evanphx/json-patch"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	chartName              = "embedded-cluster-operator"
	clusterConfigName      = "k0s"
	clusterConfigNamespace = "kube-system"
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

	operatorChart, err := getOperatorChartFromMetadata(metadata)
	if err != nil {
		return fmt.Errorf("get operator chart from metadata: %w", err)
	}

	clusterConfig, err := getExistingClusterConfig(ctx, cli)
	if err != nil {
		return fmt.Errorf("get existing clusterconfig: %w", err)
	}

	// NOTE: It is not optimal to patch the cluster config prior to upgrading the cluster because
	// the crd could be out of date. Ideally we would first run the auto-pilot upgrade and then
	// patch the cluster config, but this command is run from an ephemeral binary in the pod, and
	// when the cluster is upgraded it may no longer be available.

	err = patchClusterConfig(ctx, cli, clusterConfig, operatorChart)
	if err != nil {
		return fmt.Errorf("patch clusterconfig: %w", err)
	}

	return nil
}

func patchClusterConfig(ctx context.Context, cli client.Client, clusterConfig *k0sv1beta1.ClusterConfig, operatorChart k0sv1beta1.Chart) error {
	desired := setClusterConfigOperatorChart(clusterConfig, operatorChart)

	original, err := json.MarshalIndent(clusterConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal existing clusterconfig: %w", err)
	}

	modified, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal desired clusterconfig: %w", err)
	}

	patchData, err := jsonpatch.CreateMergePatch(original, modified)
	if err != nil {
		return fmt.Errorf("create json merge patch: %w", err)
	}

	fmt.Printf("Patching K0s cluster config with merge patch: %s\n", string(patchData))

	patch := client.RawPatch(types.MergePatchType, patchData)
	err = cli.Patch(ctx, clusterConfig, patch)
	if err != nil {
		return fmt.Errorf("patch clusterconfig: %w", err)
	}

	fmt.Println("K0s cluster config patched")

	return nil
}

func setClusterConfigOperatorChart(clusterConfig *k0sv1beta1.ClusterConfig, operatorChart k0sv1beta1.Chart) *k0sv1beta1.ClusterConfig {
	desired := clusterConfig.DeepCopy()
	if desired.Spec == nil {
		desired.Spec = &k0sv1beta1.ClusterSpec{}
	}
	if desired.Spec.Extensions == nil {
		desired.Spec.Extensions = &k0sv1beta1.ClusterExtensions{}
	}
	if desired.Spec.Extensions.Helm == nil {
		desired.Spec.Extensions.Helm = &k0sv1beta1.HelmExtensions{}
	}
	for i, chart := range desired.Spec.Extensions.Helm.Charts {
		if chart.Name == operatorChart.Name {
			desired.Spec.Extensions.Helm.Charts[i] = operatorChart
			return desired
		}
	}
	desired.Spec.Extensions.Helm.Charts = append(desired.Spec.Extensions.Helm.Charts, operatorChart)
	return desired
}

func getExistingClusterConfig(ctx context.Context, cli client.Client) (*k0sv1beta1.ClusterConfig, error) {
	clusterConfig := &k0sv1beta1.ClusterConfig{}
	err := cli.Get(ctx, client.ObjectKey{Name: clusterConfigName, Namespace: clusterConfigNamespace}, clusterConfig)
	if err != nil {
		return nil, fmt.Errorf("get chart: %w", err)
	}
	return clusterConfig, nil
}

func getOperatorChartFromMetadata(metadata *ectypes.ReleaseMetadata) (k0sv1beta1.Chart, error) {
	for _, chart := range metadata.Configs.Charts {
		if chart.Name == chartName {
			return chart, nil
		}
	}
	return k0sv1beta1.Chart{}, fmt.Errorf("chart not found")
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
