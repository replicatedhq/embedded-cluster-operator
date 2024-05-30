package registry

import (
	"context"
	"fmt"

	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
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
func MigrateRegistryData(ctx context.Context, in *clusterv1beta1.Installation, cli client.Client) error {
	hasMigrated, err := HasRegistryMigrated(ctx, cli)
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

	// check if the migration is already in progress
	// if it is, return without reattempting the migration
	migrationJob := batchv1.Job{}
	err = cli.Get(ctx, client.ObjectKey{Namespace: RegistryNamespace, Name: registryDataMigrationJobName}, &migrationJob)
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
			Namespace: RegistryNamespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Volumes: []corev1.Volume{
						{
							Name: "registry-data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "registry", // yes it's really just called "registry"
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:    "scale-down-registry",
							Image:   "bitnami/kubectl:1.29.5", // TODO make this dynamic, ensure it's included in the airgap bundle
							Command: []string{"sh", "-c"},
							Args:    []string{`kubectl scale deployment registry -n ` + RegistryNamespace + ` --replicas=0 || sleep 10000`},
						},
						{
							Name:    "wait-for-seaweed",
							Image:   "amazon/aws-cli:latest", // TODO improve this
							Command: []string{"sh", "-c"},
							Args: []string{`
         while ! aws s3 ls s3:// --endpoint-url=http://seaweedfs-s3.seaweedfs:8333; then
           echo "waiting for seaweedfs-s3 to be ready"
           sleep 5
         fi
         echo "seaweedfs-s3 is ready"
`},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: RegistryS3SecretName,
										},
									},
								},
							},
						},
						{
							Name:    "migrate-registry-data",
							Image:   "amazon/aws-cli:latest", // TODO improve this
							Command: []string{"sh", "-c"},
							Args: []string{`
         if ! aws s3 ls s3://registry --endpoint-url=http://seaweedfs-s3.seaweedfs:8333; then
           aws s3api create-bucket --bucket registry --endpoint-url=http://seaweedfs-s3.seaweedfs:8333
         fi
         aws s3 sync /var/lib/embedded-cluster/registry/ s3://registry/ --endpoint-url=http://seaweedfs-s3.seaweedfs:8333
`},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "registry-data",
									MountPath: "/var/lib/embedded-cluster/registry",
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: RegistryS3SecretName,
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "create-success-secret",
							Image:   "bitnami/kubectl:1.29.5", // TODO make this dynamic, ensure it's included in the airgap bundle
							Command: []string{"sh", "-c"},
							Args:    []string{`kubectl create secret generic -n ` + RegistryNamespace + ` ` + registryDataMigrationCompleteSecretName + `--from-literal=registry=migrated  || sleep 10000`},
						},
					},
				},
			},
		},
	}
	if err := cli.Create(ctx, &migrationJob); err != nil {
		in.Status.SetCondition(metav1.Condition{
			Type:               RegistryMigrationStatusConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             "MigrationJobFailedCreation",
			ObservedGeneration: in.Generation,
		})
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
func HasRegistryMigrated(ctx context.Context, cli client.Client) (bool, error) {
	sec := corev1.Secret{}
	err := cli.Get(ctx, client.ObjectKey{Namespace: RegistryNamespace, Name: registryDataMigrationCompleteSecretName}, &sec)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get registry migration secret: %w", err)
	}

	return true, nil
}
