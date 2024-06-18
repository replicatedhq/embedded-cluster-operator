package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"
	autopilotv1beta2 "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/artifacts"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/autopilot"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/k8sutil"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/metadata"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorChartName      = "embedded-cluster-operator"
	clusterConfigName      = "k0s"
	clusterConfigNamespace = "kube-system"
)

func Upgrade(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation, localArtifactMirrorImage string) error {
	if in.Spec.AirGap {
		err := metadata.CopyVersionMetadataToCluster(ctx, cli, in)
		if err != nil {
			return fmt.Errorf("copy version metadata to cluster: %w", err)
		}

		err = airgapDistributeArtifacts(ctx, cli, in, localArtifactMirrorImage)
		if err != nil {
			return fmt.Errorf("airgap distribute artifacts: %w", err)
		}
	}

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
	log := ctrl.LoggerFrom(ctx)

	err := cli.Get(ctx, client.ObjectKeyFromObject(in), &clusterv1beta1.Installation{})
	if err == nil {
		return nil
	} else if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("get installation: %w", err)
	}

	log.Info("Creating installation...")

	err = cli.Create(ctx, in)
	if err != nil {
		return fmt.Errorf("create installation: %w", err)
	}

	log.Info("Installation created")

	return nil
}

func applyOperatorChart(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) error {
	log := ctrl.LoggerFrom(ctx)

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

	log.Info("Waiting for operator chart to be up-to-date...")

	err = waitForOperatorChart(ctx, cli, operatorChart.Version)
	if err != nil {
		return fmt.Errorf("wait for operator chart: %w", err)
	}

	log.Info("Operator chart is up-to-date")

	return nil
}

func waitForOperatorChart(ctx context.Context, cli client.Client, version string) error {
	err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		ready, err := k8sutil.GetChartHealthVersion(ctx, cli, operatorChartName, version)
		if err != nil {
			return false, fmt.Errorf("get chart health: %w", err)
		}
		return ready, nil
	})
	return err
}

func patchClusterConfigOperatorChart(ctx context.Context, cli client.Client, clusterConfig *k0sv1beta1.ClusterConfig, operatorChart k0sv1beta1.Chart) error {
	log := ctrl.LoggerFrom(ctx)

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
		log.Info("K0s cluster config already patched")
		return nil
	}

	log.V(2).Info("Patching K0s cluster config with merge patch", "patch", string(patchData))

	patch := client.RawPatch(types.MergePatchType, patchData)
	err = cli.Patch(ctx, clusterConfig, patch)
	if err != nil {
		return fmt.Errorf("patch clusterconfig: %w", err)
	}

	log.Info("K0s cluster config patched")

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

const (
	installationNameAnnotation = "embedded-cluster.replicated.com/installation-name"
)

func airgapDistributeArtifacts(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation, localArtifactMirrorImage string) error {
	// in airgap installations let's make sure all assets have been copied to nodes.
	// this may take some time so we only move forward when 'ready'.
	err := ensureAirgapArtifactsOnNodes(ctx, cli, in, localArtifactMirrorImage)
	if err != nil {
		return fmt.Errorf("ensure airgap artifacts: %w", err)
	}

	// once all assets are in place we can create the autopilot plan to push the images to
	// containerd.
	err = ensureAirgapArtifactsInCluster(ctx, cli, in)
	if err != nil {
		return fmt.Errorf("autopilot copy airgap artifacts: %w", err)
	}

	return nil
}

func ensureAirgapArtifactsOnNodes(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation, localArtifactMirrorImage string) error {
	log := ctrl.LoggerFrom(ctx)

	log.Info("Placing artifacts on nodes...")

	err := artifacts.EnsureArtifactsJobForNodes(ctx, cli, in, localArtifactMirrorImage)
	if err != nil {
		return fmt.Errorf("ensure artifacts job for nodes: %w", err)
	}

	log.Info("Waiting for artifacts to be placed on nodes...")

	err = wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		jobs, err := artifacts.ListArtifactsJobForNodes(ctx, cli, in)
		if err != nil {
			return false, fmt.Errorf("list artifacts jobs for nodes: %w", err)
		}

		ready := true
		for nodeName, job := range jobs {
			if job == nil {
				return false, fmt.Errorf("job for node %s not found", nodeName)
			}
			if job.Status.Succeeded > 0 {
				continue
			}
			ready = false
			for _, cond := range job.Status.Conditions {
				if cond.Type == batchv1.JobFailed {
					if cond.Status == corev1.ConditionTrue {
						// fail immediately if any job fails
						return false, fmt.Errorf("job for node %s failed: %s - %s", nodeName, cond.Reason, cond.Message)
					}
					break
				}
			}
			// job is still running
		}

		return ready, nil
	})
	if err != nil {
		return fmt.Errorf("wait for artifacts job for nodes: %w", err)
	}

	log.Info("Artifacts placed on nodes")
	return nil
}

func ensureAirgapArtifactsInCluster(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) error {
	log := ctrl.LoggerFrom(ctx)

	log.Info("Uploading container images...")

	err := autopilotEnsureAirgapArtifactsPlan(ctx, cli, in)
	if err != nil {
		return fmt.Errorf("ensure autopilot plan: %w", err)
	}

	nsn := types.NamespacedName{Name: "autopilot"}
	plan := autopilotv1beta2.Plan{}

	log.Info("Waiting for container images to be uploaded...")

	err = wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, nsn, &plan)
		if err != nil {
			return false, fmt.Errorf("get autopilot plan: %w", err)
		}
		if plan.Annotations[installationNameAnnotation] != in.Name {
			return false, fmt.Errorf("autopilot plan for different installation")
		}

		switch {
		case autopilot.HasPlanSucceeded(plan):
			return true, nil
		case autopilot.HasPlanFailed(plan):
			reason := autopilot.ReasonForState(plan)
			return false, fmt.Errorf("autopilot plan failed: %s", reason)
		}
		// plan is still running
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait for autopilot plan: %w", err)
	}

	log.Info("Container images uploaded")
	return nil
}

func autopilotEnsureAirgapArtifactsPlan(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) error {
	plan, err := getAutopilotAirgapArtifactsPlan(ctx, cli, in)
	if err != nil {
		return fmt.Errorf("get autopilot airgap artifacts plan: %w", err)
	}

	err = k8sutil.EnsureObject(ctx, cli, plan, func(obj client.Object) bool {
		return obj.GetAnnotations()[installationNameAnnotation] != in.Name
	})
	if err != nil {
		return fmt.Errorf("ensure autopilot plan: %w", err)
	}

	return nil
}

func getAutopilotAirgapArtifactsPlan(ctx context.Context, cli client.Client, in *clusterv1beta1.Installation) (*autopilotv1beta2.Plan, error) {
	var commands []autopilotv1beta2.PlanCommand

	// if we are running in an airgap environment all assets are already present in the
	// node and are served by the local-artifact-mirror binary listening on localhost
	// port 50000. we just need to get autopilot to fetch the k0s binary from there.
	command, err := artifacts.CreateAutopilotAirgapPlanCommand(ctx, cli, in)
	if err != nil {
		return nil, fmt.Errorf("create autopilot airgap plan command: %w", err)
	}
	commands = append(commands, *command)

	plan := &autopilotv1beta2.Plan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: autopilotv1beta2.SchemeGroupVersion.String(),
			Kind:       "Plan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "autopilot",
			Annotations: map[string]string{
				installationNameAnnotation: in.Name,
			},
		},
		Spec: autopilotv1beta2.PlanSpec{
			Timestamp: "now",
			ID:        uuid.New().String(),
			Commands:  commands,
		},
	}

	return plan, nil
}
