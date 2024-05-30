package registry

import (
	"context"
	"fmt"

	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	appsv1 "k8s.io/api/apps/v1"
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

	registryS3CredsSecret, err := getRegistryS3SecretNameFromMetadata(metadata)
	if err != nil {
		return fmt.Errorf("get registry s3 secret name from metadata: %w", err)
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
							Args:    []string{`kubectl scale deployment registry -n ` + ns + ` --replicas=0 || sleep 10000`},
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
											Name: registryS3CredsSecret,
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
											Name: registryS3CredsSecret,
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
							Args:    []string{`kubectl create secret generic -n ` + ns + ` ` + registryDataMigrationCompleteSecretName + `--from-literal=registry=migrated  || sleep 10000`},
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

// scaleDownRegistry scales the 'registry' deployment in the provided namespace to 0 replicas.
// if it does not exist, that is an error.
func scaleDownRegistry(ctx context.Context, ns string, cli client.Client) error {
	registryDeployment := appsv1.Deployment{}
	err := cli.Get(ctx, client.ObjectKey{Namespace: ns, Name: "registry"}, &registryDeployment)
	if err != nil {
		return fmt.Errorf("get registry deployment: %w", err)
	}

	zeroVar := int32(0)

	registryDeployment.Spec.Replicas = &zeroVar
	err = cli.Update(ctx, &registryDeployment)
	if err != nil {
		return fmt.Errorf("update registry deployment: %w", err)
	}

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
