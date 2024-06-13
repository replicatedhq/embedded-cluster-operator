package upgrade

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/k8sutil"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorChartName      = "embedded-cluster-operator"
	clusterConfigName      = "k0s"
	clusterConfigNamespace = "kube-system"
)

func Upgrade(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) error {
	err := applyOperatorChart(ctx, cli, in)
	if err != nil {
		return fmt.Errorf("apply operator chart: %w", err)
	}

	// do not apply the installation if the operator chart is not up-to-date and thus the crd is
	// not up-to-date

	err = applyInstallation(ctx, cli, in)
	if err != nil {
		return fmt.Errorf("apply installation: %w", err)
	}

	return nil
}

func applyInstallation(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) error {
	err := cli.Get(ctx, types.NamespacedName{Name: in.GetName(), Namespace: in.GetNamespace()}, &clusterv1beta1.Installation{})
	if err == nil {
		return nil
	} else if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("get installation: %w", err)
	}

	fmt.Println("Creating installation...")

	err = cli.Create(ctx, in)
	if err != nil {
		return fmt.Errorf("create installation: %w", err)
	}

	fmt.Println("Installation created")

	return nil
}

func applyOperatorChart(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) error {
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

	err = patchClusterConfigOperatorChart(ctx, cli, clusterConfig, operatorChart)
	if err != nil {
		return fmt.Errorf("patch clusterconfig with operator chart: %w", err)
	}

	fmt.Println("Waiting for operator chart to be up-to-date...")

	err = waitForOperatorChart(ctx, cli, operatorChart.Version)
	if err != nil {
		return fmt.Errorf("wait for operator chart: %w", err)
	}

	fmt.Println("Operator chart is up-to-date")

	return nil
}

func waitForOperatorChart(ctx context.Context, cli client.Client, version string) error {
	for {
		err := ctx.Err()
		if err != nil {
			return err
		}

		ready, err := k8sutil.GetChartHealthVersion(ctx, cli, operatorChartName, version)
		if err != nil {
			return fmt.Errorf("get chart health: %w", err)
		}

		if ready {
			return nil
		}
	}
}

func patchClusterConfigOperatorChart(ctx context.Context, cli client.Client, clusterConfig *k0sv1beta1.ClusterConfig, operatorChart k0sv1beta1.Chart) error {
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

	if string(patchData) == "{}" {
		fmt.Println("K0s cluster config already patched")
		return nil
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
		if chart.Name == operatorChartName {
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
		if chart.Name == operatorChartName {
			return chart, nil
		}
	}
	return k0sv1beta1.Chart{}, fmt.Errorf("chart not found")
}
