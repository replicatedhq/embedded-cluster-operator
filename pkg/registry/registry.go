package registry

import (
	"context"
	"encoding/json"
	"fmt"

	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const (
	// SeaweedfsS3SecretReadyConditionType represents the condition type that indicates status of
	// the Seaweedfs secret.
	SeaweedfsS3SecretReadyConditionType = "SeaweedfsS3SecretReady"

	// RegistryS3SecretReadyConditionType represents the condition type that indicates status of
	// the Registry secret.
	RegistryS3SecretReadyConditionType = "RegistryS3SecretReady"
)

func EnsureSecrets(ctx context.Context, in *clusterv1beta1.Installation, metadata *ectypes.ReleaseMetadata, cli client.Client) error {
	log := ctrl.LoggerFrom(ctx)

	config, op, err := ensureSeaweedfsS3Secret(ctx, in, metadata, cli)
	if err != nil {
		in.Status.SetCondition(metav1.Condition{
			Type:               SeaweedfsS3SecretReadyConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             "SecretFailed",
			Message:            err.Error(),
			ObservedGeneration: in.Generation,
		})
		return fmt.Errorf("ensure seaweedfs s3 secret: %w", err)
	} else if op != controllerutil.OperationResultNone {
		log.Info("Seaweedfs s3 secret changed", "operation", op)
	}
	in.Status.SetCondition(metav1.Condition{
		Type:               SeaweedfsS3SecretReadyConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             "SecretReady",
		ObservedGeneration: in.Generation,
	})

	op, err = ensureRegistryS3Secret(ctx, in, metadata, cli, config)
	if err != nil {
		in.Status.SetCondition(metav1.Condition{
			Type:               RegistryS3SecretReadyConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             "SecretFailed",
			Message:            err.Error(),
			ObservedGeneration: in.Generation,
		})
		return fmt.Errorf("ensure registry s3 secret: %w", err)
	} else if op != controllerutil.OperationResultNone {
		log.Info("Registry s3 secret changed", "operation", op)
	}
	in.Status.SetCondition(metav1.Condition{
		Type:               RegistryS3SecretReadyConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             "SecretReady",
		ObservedGeneration: in.Generation,
	})

	return nil
}

func ensureSeaweedfsS3Secret(ctx context.Context, in *clusterv1beta1.Installation, metadata *ectypes.ReleaseMetadata, cli client.Client) (*seaweedfsConfig, controllerutil.OperationResult, error) {
	log := ctrl.LoggerFrom(ctx)

	op := controllerutil.OperationResultNone

	namespace, err := getSeaweedfsNamespaceFromMetadata(metadata)
	if err != nil {
		return nil, op, fmt.Errorf("get seaweedfs namespace from metadata: %w", err)
	}

	secretName, err := getSeaweedfsS3SecretNameFromMetadata(metadata)
	if err != nil {
		return nil, op, fmt.Errorf("get seaweedfs s3 secret name from metadata: %w", err)
	}

	err = ensureSeaweedfsNamespace(ctx, cli, namespace)
	if err != nil {
		return nil, op, fmt.Errorf("ensure seaweedfs namespace: %w", err)
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
	}

	var config seaweedfsConfig

	op, err = ctrl.CreateOrUpdate(ctx, cli, obj, func() error {
		err := ctrl.SetControllerReference(in, obj, cli.Scheme())
		if err != nil {
			return fmt.Errorf("set controller reference: %w", err)
		}

		if obj.Data != nil {
			err := json.Unmarshal(obj.Data["seaweedfs_s3_config"], &config)
			if err != nil {
				log.Error(err, "Unmarshal seaweedfs_s3_config failed, will recreate the secret")
			}
		}

		var changed bool
		if _, ok := config.getCredentials("anvAdmin"); !ok {
			config.Identities = append(config.Identities, seaweedfsIdentity{
				Name: "anvAdmin",
				Credentials: []seaweedfsIdentityCredential{{
					AccessKey: randString(20),
					SecretKey: randString(40),
				}},
				Actions: []string{"Admin", "Read", "Write"},
			})
			changed = true
		}
		if _, ok := config.getCredentials("anvReadOnly"); !ok {
			config.Identities = append(config.Identities, seaweedfsIdentity{
				Name: "anvReadOnly",
				Credentials: []seaweedfsIdentityCredential{{
					AccessKey: randString(20),
					SecretKey: randString(40),
				}},
				Actions: []string{"Read"},
			})
			changed = true
		}
		if !changed {
			return nil
		}

		configData, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("marshal seaweedfs_s3_config: %w", err)
		}

		if obj.Data == nil {
			obj.Data = make(map[string][]byte)
		}
		obj.Data["seaweedfs_s3_config"] = configData

		return nil
	})
	if err != nil {
		return nil, op, fmt.Errorf("create or update seaweedfs s3 secret: %w", err)
	}

	return &config, op, nil
}

func ensureSeaweedfsNamespace(ctx context.Context, cli client.Client, namespace string) error {
	obj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	err := cli.Create(ctx, obj)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create seaweedfs namespace: %w", err)

	}

	return nil
}

func ensureRegistryS3Secret(ctx context.Context, in *clusterv1beta1.Installation, metadata *ectypes.ReleaseMetadata, cli client.Client, sfsConfig *seaweedfsConfig) (controllerutil.OperationResult, error) {
	op := controllerutil.OperationResultNone

	sfsCreds, ok := sfsConfig.getCredentials("anvAdmin")
	if !ok {
		return op, fmt.Errorf("seaweedfs s3 anvAdmin credentials not found")
	}

	namespace, err := getRegistryNamespaceFromMetadata(metadata)
	if err != nil {
		return op, fmt.Errorf("get registry namespace from metadata: %w", err)
	}

	secretName, err := getRegistryS3SecretNameFromMetadata(metadata)
	if err != nil {
		return op, fmt.Errorf("get registry s3 secret name from metadata: %w", err)
	}

	err = ensureRegistryNamespace(ctx, cli, namespace)
	if err != nil {
		return op, fmt.Errorf("ensure registry namespace: %w", err)
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
	}

	op, err = ctrl.CreateOrUpdate(ctx, cli, obj, func() error {
		err := ctrl.SetControllerReference(in, obj, cli.Scheme())
		if err != nil {
			return fmt.Errorf("set controller reference: %w", err)
		}

		if obj.Data == nil {
			obj.Data = make(map[string][]byte)
		}
		obj.Data["s3AccessKey"] = []byte(sfsCreds.AccessKey)
		obj.Data["s3SecretKey"] = []byte(sfsCreds.SecretKey)

		return nil
	})
	if err != nil {
		return op, fmt.Errorf("create or update registry s3 secret: %w", err)
	}

	return op, nil
}

func ensureRegistryNamespace(ctx context.Context, cli client.Client, namespace string) error {
	obj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	err := cli.Create(ctx, obj)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create registry namespace: %w", err)
	}

	return nil
}

func getSeaweedfsNamespaceFromMetadata(metadata *ectypes.ReleaseMetadata) (string, error) {
	chart, err := getSeaweedfsChartFromMetadata(metadata)
	if err != nil {
		return "", fmt.Errorf("get seaweedfs charts settings from metadata: %w", err)
	}
	return chart.TargetNS, nil
}

func getSeaweedfsS3SecretNameFromMetadata(metadata *ectypes.ReleaseMetadata) (string, error) {
	chart, err := getSeaweedfsChartFromMetadata(metadata)
	if err != nil {
		return "", fmt.Errorf("get seaweedfs chart from metadata: %w", err)
	}
	var valuesStruct struct {
		Filer struct {
			S3 struct {
				ExistingConfigSecret string `json:"existingConfigSecret"`
			} `json:"s3"`
		} `json:"filer"`
	}
	err = yaml.Unmarshal([]byte(chart.Values), &valuesStruct)
	if err != nil {
		return "", fmt.Errorf("unmarshal chart values: %w", err)
	}
	if valuesStruct.Filer.S3.ExistingConfigSecret == "" {
		return "", fmt.Errorf("secret ref not found")
	}
	return valuesStruct.Filer.S3.ExistingConfigSecret, nil
}

func getSeaweedfsChartFromMetadata(metadata *ectypes.ReleaseMetadata) (*k0sv1beta1.Chart, error) {
	config, ok := metadata.BuiltinConfigs["seaweedfs"]
	if !ok {
		return nil, fmt.Errorf("config not found")
	}
	if len(config.Charts) == 0 {
		return nil, fmt.Errorf("chart not found")
	}
	return &config.Charts[0], nil
}

func getRegistryNamespaceFromMetadata(metadata *ectypes.ReleaseMetadata) (string, error) {
	chart, err := getRegistryChartFromMetadata(metadata)
	if err != nil {
		return "", fmt.Errorf("get registry chart from metadata: %w", err)
	}
	return chart.TargetNS, nil
}

func getRegistryS3SecretNameFromMetadata(metadata *ectypes.ReleaseMetadata) (string, error) {
	chart, err := getRegistryChartFromMetadata(metadata)
	if err != nil {
		return "", fmt.Errorf("get registry chart from metadata: %w", err)
	}
	var valuesStruct struct {
		Secrets struct {
			S3 struct {
				SecretRef string `json:"secretRef"`
			} `json:"s3"`
		} `json:"secrets"`
	}
	err = yaml.Unmarshal([]byte(chart.Values), &valuesStruct)
	if err != nil {
		return "", fmt.Errorf("unmarshal chart values: %w", err)
	}
	if valuesStruct.Secrets.S3.SecretRef == "" {
		return "", fmt.Errorf("secret ref not found")
	}
	return valuesStruct.Secrets.S3.SecretRef, nil
}

func getRegistryChartFromMetadata(metadata *ectypes.ReleaseMetadata) (*k0sv1beta1.Chart, error) {
	config, ok := metadata.BuiltinConfigs["registry-ha"]
	if !ok {
		return nil, fmt.Errorf("config not found")
	}
	if len(config.Charts) == 0 {
		return nil, fmt.Errorf("chart not found")
	}
	return &config.Charts[0], nil
}
