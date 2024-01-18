package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/k0sproject/dig"
	k0shelm "github.com/k0sproject/k0s/pkg/apis/helm/v1beta1"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"github.com/ohler55/ojg/jp"
	"github.com/ohler55/ojg/oj"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/replicatedhq/embedded-cluster-operator/api/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
)

// MergeValues takes two helm values in the form of dig.Mapping{} and a list of values (in jsonpath notation) to not override
// and combines the values. it returns the resultant yaml string
func MergeValues(oldValues, newValues string, protectedValues []string) (string, error) {

	newValuesMap := dig.Mapping{}
	if err := yaml.Unmarshal([]byte(newValues), &newValuesMap); err != nil {
		return "", fmt.Errorf("failed to unmarshal new chart values: %w", err)
	}

	// merge the known fields from the current chart values to the new chart values
	for _, path := range protectedValues {
		x, err := jp.ParseString(path)
		if err != nil {
			return "", fmt.Errorf("failed to parse json path: %w", err)
		}

		valuesJson, err := yaml.YAMLToJSON([]byte(oldValues))
		if err != nil {
			return "", fmt.Errorf("failed to convert yaml to json: %w", err)
		}

		obj, err := oj.ParseString(string(valuesJson))
		if err != nil {
			return "", fmt.Errorf("failed to parse json: %w", err)
		}

		value := x.Get(obj)

		// if the value is empty, skip it
		if len(value) < 1 {
			continue
		}

		err = x.Set(newValuesMap, value[0])
		if err != nil {
			return "", fmt.Errorf("failed to set json path: %w", err)
		}
	}

	newValuesYaml, err := yaml.Marshal(newValuesMap)
	if err != nil {
		return "", fmt.Errorf("failed to marshal new chart values: %w", err)
	}
	return string(newValuesYaml), nil

}

// ReconcileHelmCharts reconciles the helm charts from the Installation metadata with the clusterconfig object.
func (r *InstallationReconciler) ReconcileHelmCharts(ctx context.Context, in *v1beta1.Installation) error {
	if in.Spec.Config == nil || in.Spec.Config.Version == "" {
		if in.Status.State == v1beta1.InstallationStateKubernetesInstalled {
			in.Status.SetState(v1beta1.InstallationStateInstalled, "Installed")
		}
		return nil
	}

	if in.Status.State == v1beta1.InstallationStateFailed {
		return nil
	}

	log := ctrl.LoggerFrom(ctx)
	meta, err := release.MetadataFor(ctx, in.Spec.Config.Version, in.Spec.MetricsBaseURL)
	if err != nil {
		in.Status.SetState(v1beta1.InstallationStateHelmChartUpdateFailure, err.Error())
		return nil
	}
	// skip if the new release has no addon configs
	if meta.Configs == nil && in.Spec.Config.Extensions.Helm == nil {
		log.Info("addons", "configcheck", "no addons")
		if in.Status.State == v1beta1.InstallationStateKubernetesInstalled {
			in.Status.SetState(v1beta1.InstallationStateInstalled, "Installed")
		}
		return nil
	}

	log.Info("reconciling helm charts", "defaultChartCount", len(meta.Configs.Charts), "customChartCount", len(in.Spec.Config.Extensions.Helm.Charts))

	combinedConfigs := mergeHelmConfigs(meta, in)

	// skip if installer is already complete
	if in.Status.State == v1beta1.InstallationStateInstalled {
		return nil
	}
	// We want to skip and requeue if the k0s upgrade is still in progress
	if !in.Status.GetKubernetesInstalled() {
		return nil
	}

	// detect drift between the cluster config and the installer metadata
	var installedCharts k0shelm.ChartList
	if err := r.List(ctx, &installedCharts); err != nil {
		return fmt.Errorf("failed to list installed charts: %w", err)
	}
	chartErrors, chartDrift := detectChartDrift(ctx, combinedConfigs, installedCharts)

	// If all addons match their target version, mark installation as complete (or as failed, if there are errors)
	if !chartDrift {
		log.Info("no chart drift")
		// If any chart has errors, update installer state and return
		if len(chartErrors) > 0 {
			chartErrorString := strings.Join(chartErrors, ",")
			chartErrorString = "failed to update helm charts: " + chartErrorString
			log.Info("chart errors!", "errors", chartErrorString)
			if len(chartErrorString) > 1024 {
				chartErrorString = chartErrorString[:1024]
			}
			in.Status.SetState(v1beta1.InstallationStateHelmChartUpdateFailure, chartErrorString)
			return nil
		}
		in.Status.SetState(v1beta1.InstallationStateInstalled, "Addons upgraded")
		return nil
	}

	// fetch the current clusterconfig
	var clusterconfig k0sv1beta1.ClusterConfig
	if err := r.Get(ctx, client.ObjectKey{Name: "k0s", Namespace: "kube-system"}, &clusterconfig); err != nil {
		return fmt.Errorf("failed to get cluster config: %w", err)
	}

	finalChartList, err := generateDesiredCharts(meta, clusterconfig, combinedConfigs)
	if err != nil {
		return err
	}

	// Replace the current chart configs with the new chart configs
	clusterconfig.Spec.Extensions.Helm.Charts = finalChartList
	clusterconfig.Spec.Extensions.Helm.Repositories = combinedConfigs.Repositories
	clusterconfig.Spec.Extensions.Helm.ConcurrencyLevel = combinedConfigs.ConcurrencyLevel
	in.Status.SetState(v1beta1.InstallationStateAddonsInstalling, "Installing addons")
	log.Info("updating charts in cluster config", "config spec", clusterconfig.Spec)
	//Update the clusterconfig
	if err := r.Update(ctx, &clusterconfig); err != nil {
		return fmt.Errorf("failed to update cluster config: %w", err)
	}
	return nil
}

func mergeHelmConfigs(meta *release.Meta, in *v1beta1.Installation) *k0sv1beta1.HelmExtensions {
	// merge default helm charts (from meta.Configs) with vendor helm charts (from in.Spec.Config.Extensions.Helm)
	combinedConfigs := &k0sv1beta1.HelmExtensions{ConcurrencyLevel: 1}
	if meta.Configs != nil {
		combinedConfigs = meta.Configs
	}
	if in.Spec.Config.Extensions.Helm != nil {
		// set the concurrency level to the minimum of our default and the user provided value
		if in.Spec.Config.Extensions.Helm.ConcurrencyLevel > 0 {
			combinedConfigs.ConcurrencyLevel = min(in.Spec.Config.Extensions.Helm.ConcurrencyLevel, combinedConfigs.ConcurrencyLevel)
		}

		// append the user provided charts to the default charts
		combinedConfigs.Charts = append(combinedConfigs.Charts, in.Spec.Config.Extensions.Helm.Charts...)
		// append the user provided repositories to the default repositories
		combinedConfigs.Repositories = append(combinedConfigs.Repositories, in.Spec.Config.Extensions.Helm.Repositories...)
	}
	return combinedConfigs
}

func detectChartDrift(ctx context.Context, combinedConfigs *k0sv1beta1.HelmExtensions, installedCharts k0shelm.ChartList) ([]string, bool) {
	log := ctrl.LoggerFrom(ctx)

	targetCharts := combinedConfigs.Charts
	chartErrors := []string{}
	chartDrift := false
	if len(installedCharts.Items) != len(targetCharts) { // if the desired numbers of charts are different, there is drift
		log.Info("numbers of charts differ", "installed", installedCharts.Items, "target", targetCharts)
		chartDrift = true
	}
	// grab the installed charts
	for _, chart := range installedCharts.Items {
		// extract any errors from installed charts
		if chart.Status.Error != "" {
			chartErrors = append(chartErrors, chart.Status.Error)
		}
		// check for version drift between installed charts and charts in the installer metadata
		chartSeen := false
		for _, targetChart := range targetCharts {
			if targetChart.Name != chart.Status.ReleaseName {
				continue
			}
			chartSeen = true
			if targetChart.Version != chart.Spec.Version {
				log.Info(fmt.Sprintf("chart %q version differs - %q != %q", targetChart.Name, targetChart.Version, chart.Spec.Version))
				chartDrift = true
			}
		}
		if !chartSeen { // if this chart in the cluster is not in the target spec, there is drift
			chartDrift = true
		}
	}
	return chartErrors, chartDrift
}

func generateDesiredCharts(meta *release.Meta, clusterconfig k0sv1beta1.ClusterConfig, combinedConfigs *k0sv1beta1.HelmExtensions) ([]k0sv1beta1.Chart, error) {
	// get the protected values from the release metadata
	protectedValues := map[string][]string{}
	if meta.Protected != nil {
		protectedValues = meta.Protected
	}

	// TODO - apply unsupported override from installation config
	finalConfigs := map[string]k0sv1beta1.Chart{}
	// include charts in the final spec that are already in the cluster (with merged values)
	for _, chart := range clusterconfig.Spec.Extensions.Helm.Charts {
		for _, newChart := range combinedConfigs.Charts {
			// check if we can skip this chart
			_, ok := protectedValues[chart.Name]
			if chart.Name != newChart.Name || !ok {
				continue
			}
			// if we have known fields, we need to merge them forward
			newValuesYaml, err := MergeValues(chart.Values, newChart.Values, protectedValues[chart.Name])
			if err != nil {
				return nil, fmt.Errorf("failed to merge chart values: %w", err)
			}
			newChart.Values = newValuesYaml
			finalConfigs[newChart.Name] = newChart
			break
		}
	}
	// include new charts in the final spec that are not yet in the cluster
	for _, newChart := range combinedConfigs.Charts {
		if _, ok := finalConfigs[newChart.Name]; !ok {
			finalConfigs[newChart.Name] = newChart
		}
	}

	// flatten chart map
	finalChartList := []k0sv1beta1.Chart{}
	for _, chart := range finalConfigs {
		finalChartList = append(finalChartList, chart)
	}
	return finalChartList, nil
}
