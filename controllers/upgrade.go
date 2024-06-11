package controllers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	selfUpgradeTimeout = 5 * time.Minute
)

// maybeSelfUpgrade checks if the embedded cluster version has changed and runs the upgrade command
// if needed. If the version has changed, the process will exit.
func (r *InstallationReconciler) maybeSelfUpgrade(ctx context.Context, in *v1beta1.Installation) {
	curstr := strings.TrimPrefix(os.Getenv("EMBEDDEDCLUSTER_VERSION"), "v")
	desstr := strings.TrimPrefix(in.Spec.Config.Version, "v")
	if curstr == desstr {
		return
	}

	log := ctrl.LoggerFrom(ctx)
	logArgs := []interface{}{"currentVersion", curstr, "desiredVersion", desstr}

	log.Info("Embedded cluster version has changed, upgrading...", logArgs...)

	err := r.selfUpgrade(ctx, in)
	if err != nil {
		log.Error(err, "Failed to upgrade", logArgs...)
		os.Exit(1)
	}

	log.Info("Upgrade completed successfully", logArgs...)
	os.Exit(0)
}

func (r *InstallationReconciler) selfUpgrade(ctx context.Context, in *v1beta1.Installation) error {
	log := ctrl.LoggerFrom(ctx)

	log.Info("Downloading upgrade binary...")

	bin, err := downloadUpgradeBinary(ctx)
	if err != nil {
		return fmt.Errorf("download upgrade binary: %w", err)
	}

	log.Info("Running upgrade command...")

	ctx, cancel := context.WithTimeout(ctx, selfUpgradeTimeout)
	defer cancel()

	// TODO(upgrade): do not hardcode the installation secret name
	cmd := exec.CommandContext(ctx, bin, "upgrade", "--installation-secret", "upgrade-spec")
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Info("Upgrade command output:")
		for _, line := range strings.Split(string(out), "\n") {
			log.Info("  " + line)
		}
	}
	if err != nil {
		return fmt.Errorf("run upgrade command: %w", err)
	}
	return nil
}

func downloadUpgradeBinary(ctx context.Context) (string, error) {
	// TODO
	return os.Args[0], nil
}
