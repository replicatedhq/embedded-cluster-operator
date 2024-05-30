package registry

import (
	"context"
	"fmt"

	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const registryDataMigrationCompleteSecretName = "registry-data-migration-complete"
const registryDataMigrationJobName = "registry-data-migration"

const RegistryMigrationStatusConditionType = "RegistryMigrationStatus"

// MigrateRegistryData should be called when transitioning from non-HA to HA airgapped installations
// this function scales down the registry deployment to 0 replicas, then creates a job that will migrate the data before
// creating a 'migration is complete' secret in the registry namespace
// if this secret is present, the function will return without reattempting the migration
func MigrateRegistryData(ctx context.Context, in *clusterv1beta1.Installation, metadata *ectypes.ReleaseMetadata, cli client.Client) error {
	hasMigrated, err := HasRegistryMigrated(ctx, metadata, cli)
	if err != nil {
		return fmt.Errorf("check if registry has migrated before running migration: %w", err)
	}
	if hasMigrated {
		in.Status.SetCondition(metav1.Condition{
			Type:               RegistryMigrationStatusConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             "MigrationJobCompleted",
			ObservedGeneration: in.Generation,
		})
		return nil
	}

	ns, err := getRegistryNamespaceFromMetadata(metadata)
	if err != nil {
		return fmt.Errorf("get registry namespace from metadata: %w", err)
	}

	// check if the migration is already in progress
	// if it is, return without reattempting the migration
	migrationJob := batchv1.Job{}
	err = cli.Get(ctx, client.ObjectKey{Namespace: ns, Name: registryDataMigrationJobName}, &migrationJob)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("get migration job: %w", err)
		}
	} else {
		if migrationJob.Status.Active > 0 {
			return nil
		}
		if migrationJob.Status.Failed > 0 {
			in.Status.SetCondition(metav1.Condition{
				Type:               RegistryMigrationStatusConditionType,
				Status:             metav1.ConditionFalse,
				Reason:             "MigrationJobFailed",
				ObservedGeneration: in.Generation,
			})
			return fmt.Errorf("registry migration job failed")
		}
		// TODO: handle other conditions
		return nil
	}

	// create the migration job
	migrationJob = batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      registryDataMigrationJobName,
			Namespace: ns,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		Spec: batchv1.JobSpec{},
	}
	if err := cli.Create(ctx, &migrationJob); err != nil {
		return fmt.Errorf("create migration job: %w", err)
	}

	in.Status.SetCondition(metav1.Condition{
		Type:               RegistryMigrationStatusConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             "MigrationJobInProgress",
		ObservedGeneration: in.Generation,
	})

	return nil
}

// HasRegistryMigrated checks if the registry data has been migrated by looking for the 'migration complete' secret in the registry namespace
func HasRegistryMigrated(ctx context.Context, metadata *ectypes.ReleaseMetadata, cli client.Client) (bool, error) {
	ns, err := getRegistryNamespaceFromMetadata(metadata)
	if err != nil {
		return false, fmt.Errorf("get registry namespace from metadata: %w", err)
	}

	sec := corev1.Secret{}
	err = cli.Get(ctx, client.ObjectKey{Namespace: ns, Name: registryDataMigrationCompleteSecretName}, &sec)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get registry migration secret: %w", err)
	}

	return true, nil
}
