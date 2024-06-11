package controllers

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
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

	log.Info("Embedded cluster version has changed, running upgrade command", "currentVersion", curstr, "desiredVersion", desstr)

	cmd := exec.Command(os.Args[0], "upgrade")

	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Info("Upgrade command output:")
		for _, line := range strings.Split(string(out), "\n") {
			log.Info("  " + line)
		}
	}

	if err == nil {
		log.Info("Upgrade command completed successfully", "currentVersion", curstr, "desiredVersion", desstr)
		os.Exit(0)
	}

	log.Error(err, "Failed to run upgrade command", "currentVersion", curstr, "desiredVersion", desstr)
	os.Exit(1)
}
