package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	apv1b2 "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	"github.com/k0sproject/version"
	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/autopilot"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileK0sVersion reconciles the k0s version in the Installation object status. If the
// Installation spec.config points to a different version we start an upgrade Plan. If an
// upgrade plan already exists we make sure the installation status is updated with the
// latest plan status.
func (r *InstallationReconciler) ReconcileK0sVersion(ctx context.Context, in *v1beta1.Installation) error {
	// starts by checking if this is the unique installation object in the cluster. if
	// this is true then we don't need to sync anything as this is part of the initial
	// cluster installation.
	uniqinst, err := r.HasOnlyOneInstallation(ctx)
	if err != nil {
		return fmt.Errorf("failed to find if there are multiple installations: %w", err)
	}

	// if the installation has no desired version then there isn't much we can do other
	// than flagging as installed. if there is also only one installation object in the
	// cluster then there is no upgrade to be executed, just set it to Installed and
	// move on.
	if in.Spec.Config == nil || in.Spec.Config.Version == "" || uniqinst {
		in.Status.SetState(v1beta1.InstallationStateKubernetesInstalled, "", nil)
		return nil
	}

	// in airgap installation the first thing we need to do is to ensure that the embedded
	// cluster version metadata is available inside the cluster. we can't use the internet
	// to fetch it directly from our remote servers.
	if in.Spec.AirGap {
		if err := r.CopyVersionMetadataToCluster(ctx, in); err != nil {
			return fmt.Errorf("failed to copy version metadata to cluster: %w", err)
		}
	}

	// fetch the metadata for the desired embedded cluster version.
	meta, err := release.MetadataFor(ctx, in, r.Client)
	if err != nil {
		in.Status.SetState(v1beta1.InstallationStateFailed, err.Error(), nil)
		return nil
	}

	// find out the kubernetes version we are currently running so we can compare with
	// the desired kubernetes version. we don't want anyone trying to do a downgrade.
	vinfo, err := r.Discovery.ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to get server version: %w", err)
	}
	runningVersion := vinfo.GitVersion
	running, err := version.NewVersion(runningVersion)
	if err != nil {
		reason := fmt.Sprintf("Invalid running version %s", runningVersion)
		in.Status.SetState(v1beta1.InstallationStateFailed, reason, nil)
		return nil
	}

	// if we have installed the cluster with a k0s version like v1.29.1+k0s.1 then
	// the kubernetes server version reported back is v1.29.1+k0s. i.e. the .1 is
	// not part of the kubernetes version, it is the k0s version. we trim it down
	// so we can compare kube with kube version.
	desiredVersion := meta.Versions["Kubernetes"]
	desired, err := k8sServerVersionFromK0sVersion(desiredVersion)
	if err != nil {
		reason := fmt.Sprintf("Invalid desired version %s", desiredVersion)
		in.Status.SetState(v1beta1.InstallationStateFailed, reason, nil)
		return nil
	}

	// stop here if someone is trying a downgrade. we do not support this, flag the
	// installation accordingly and returns.
	if running.GreaterThan(desired) {
		in.Status.SetState(v1beta1.InstallationStateFailed, "Downgrades not supported", nil)
		return nil
	}

	if in.Spec.AirGap {
		// in airgap installations let's make sure all assets have been copied to nodes.
		// this may take some time so we only move forward when 'ready'.
		if ready, err := r.CopyArtifactsToNodes(ctx, in); err != nil {
			return fmt.Errorf("failed to copy artifacts to nodes: %w", err)
		} else if !ready {
			return nil
		}
	}

	var plan apv1b2.Plan
	okey := client.ObjectKey{Name: "autopilot"}
	if err := r.Get(ctx, okey, &plan); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get upgrade plan: %w", err)
	} else if errors.IsNotFound(err) {
		// there is no autopilot plan in the cluster so we are free to
		// start our own plan. here we link the plan to the installation
		// by its name.
		if err := r.StartAutopilotUpgrade(ctx, in); err != nil {
			return fmt.Errorf("failed to start upgrade: %w", err)
		}
		return nil
	}

	// if we have created this plan we just found for the installation we are
	// reconciling we set the installation state according to the plan state.
	// we check both the plan id and an annotation inside the plan. the usage
	// of the plan id is deprecated in favour of the annotation.
	annotation := plan.Annotations[InstallationNameAnnotation]
	if annotation == in.Name || plan.Spec.ID == in.Name {
		r.SetStateBasedOnPlan(in, plan)
		return nil
	}

	// this is most likely a plan that has been created by a previous installation
	// object, we can't move on until this one finishes. this can happen if someone
	// issues multiple upgrade requests at the same time.
	if !autopilot.HasThePlanEnded(plan) {
		reason := fmt.Sprintf("Another upgrade is in progress (%s)", plan.Spec.ID)
		in.Status.SetState(v1beta1.InstallationStateWaiting, reason, nil)
		return nil
	}

	// it seems like the plan previously created by other installation object
	// has been finished, we can delete it. this will trigger a new reconcile
	// this time without the plan (i.e. we will be able to create our own plan).
	if err := r.Delete(ctx, &plan); err != nil {
		return fmt.Errorf("failed to delete previous upgrade plan: %w", err)
	}
	return nil
}

// StartAutopilotUpgrade creates an autopilot plan to upgrade to version specified in spec.config.version.
func (r *InstallationReconciler) StartAutopilotUpgrade(ctx context.Context, in *clusterv1beta1.Installation) error {
	targets, err := r.DetermineUpgradeTargets(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine upgrade targets: %w", err)
	}
	meta, err := release.MetadataFor(ctx, in, r.Client)
	if err != nil {
		return fmt.Errorf("failed to get release bundle: %w", err)
	}

	k0surl := fmt.Sprintf(
		"%s/embedded-cluster-public-files/k0s-binaries/%s",
		in.Spec.MetricsBaseURL,
		meta.Versions["Kubernetes"],
	)

	// we need to assess what commands should autopilot run upon this upgrade. we can have four
	// different scenarios: 1) we are upgrading only the airgap artifacts, 2) we are upgrading
	// only k0s binaries, 3) we are upgrading both, 4) we are upgrading neither. we populate the
	// 'commands' slice with the commands necessary to execute these operations.
	var commands []apv1b2.PlanCommand

	if in.Spec.AirGap {
		// if we are running in an airgap environment all assets are already present in the
		// node and are served by the local-artifact-mirror binary listening on localhost
		// port 50000. we just need to get autopilot to fetch the k0s binary from there.
		k0surl = "http://127.0.0.1:50000/bin/k0s-upgrade"
		command, err := r.CreateAirgapPlanCommand(ctx, in)
		if err != nil {
			return fmt.Errorf("failed to create airgap plan command: %w", err)
		}
		commands = append(commands, *command)
	}

	// if the kubernetes version has changed we create an upgrade command
	shouldUpgrade, err := r.shouldUpgradeK0s(ctx, in, meta.Versions["Kubernetes"])
	if err != nil {
		return fmt.Errorf("failed to determine if k0s should be upgraded: %w", err)
	}
	if shouldUpgrade {
		commands = append(commands, apv1b2.PlanCommand{
			K0sUpdate: &apv1b2.PlanCommandK0sUpdate{
				Version: meta.Versions["Kubernetes"],
				Targets: targets,
				Platforms: apv1b2.PlanPlatformResourceURLMap{
					"linux-amd64": {URL: k0surl, Sha256: meta.K0sSHA},
				},
			},
		})
	}

	// if no airgap nor k0s upgrade has been defined it means we are up to date so we set
	// the installation state to 'Installed' and return. no extra autopilot plan creation
	// is necessary at this stage.
	if len(commands) == 0 {
		in.Status.SetState(clusterv1beta1.InstallationStateKubernetesInstalled, "", nil)
		return nil
	}

	plan := apv1b2.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name: "autopilot",
			Annotations: map[string]string{
				InstallationNameAnnotation: in.Name,
			},
		},
		Spec: apv1b2.PlanSpec{
			Timestamp: "now",
			ID:        uuid.New().String(),
			Commands:  commands,
		},
	}
	if err := r.Create(ctx, &plan); err != nil {
		return fmt.Errorf("failed to create upgrade plan: %w", err)
	}
	in.Status.SetState(clusterv1beta1.InstallationStateEnqueued, "", nil)
	return nil
}

func (r *InstallationReconciler) shouldUpgradeK0s(ctx context.Context, in *clusterv1beta1.Installation, desiredK0sVersion string) (bool, error) {
	// if the kubernetes version has changed we create an upgrade command.
	serverVersion, err := r.Discovery.ServerVersion()
	if err != nil {
		return false, fmt.Errorf("get server version: %w", err)
	}
	runningServerVersion, err := version.NewVersion(serverVersion.GitVersion)
	if err != nil {
		return false, fmt.Errorf("parse running server version: %w", err)
	}
	desiredServerVersion, err := k8sServerVersionFromK0sVersion(desiredK0sVersion)
	if err != nil {
		return false, fmt.Errorf("parse desired server version: %w", err)
	}
	if desiredServerVersion.GreaterThan(runningServerVersion) {
		return true, nil
	} else if desiredServerVersion.LessThan(runningServerVersion) {
		return false, nil
	}

	// if this is the same server version we may be able to tell the actual k0s version from the
	// previous installation
	previousK0sVersion, err := r.discoverPreviousK0sVersion(ctx, in)
	if err != nil {
		return false, fmt.Errorf("discover previous k0s version: %w", err)
	}
	return previousK0sVersion != "" && desiredK0sVersion != previousK0sVersion, nil
}

// discoverPreviousK0sVersion gets the k0s version from the previous installation object.
func (r *InstallationReconciler) discoverPreviousK0sVersion(ctx context.Context, in *clusterv1beta1.Installation) (string, error) {
	ins, err := r.listInstallations(ctx)
	if err != nil {
		return "", fmt.Errorf("list installations: %w", err)
	}
	for _, i := range ins {
		if i.Name == in.Name {
			continue
		}
		// the previous installation should be the second one in the list
		meta, err := release.MetadataFor(ctx, &i, r.Client)
		if err != nil {
			return "", fmt.Errorf("get release metadata for installation %s: %w", i.Name, err)
		}
		if v := meta.Versions["Kubernetes"]; v != "" {
			return v, nil
		}
		return "", nil
	}
	return "", nil
}

// if we have installed the cluster with a k0s version like v1.29.1+k0s.1 then
// the kubernetes server version reported back is v1.29.1+k0s. i.e. the .1 is
// not part of the kubernetes version, it is the k0s version. we trim it down
// so we can compare kube with kube version.
func k8sServerVersionFromK0sVersion(k0sVersion string) (*version.Version, error) {
	index := strings.Index(k0sVersion, "+k0s")
	if index == -1 {
		return nil, fmt.Errorf("invalid k0s version")
	}
	k0sVersion = k0sVersion[:index+len("+k0s")]
	v, err := version.NewVersion(k0sVersion)
	if err != nil {
		return nil, fmt.Errorf("parse k0s version: %w", err)
	}
	return v, nil
}
