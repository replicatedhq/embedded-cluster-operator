package cli

import (
	"context"
	"fmt"

	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/k8sutil"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/upgrade"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultInstallationSecretNamespace = "embedded-cluster"
	DefaultInstallationSecretKey       = "installation.yaml"
)

// UpgradeCmd returns a cobra command for upgrading the embedded cluster operator.
// It is called by KOTS admin console to upgrade the embedded cluster operator and installation.
func UpgradeCmd() *cobra.Command {
	var secretName, secretNamespace, secretKey string

	cmd := &cobra.Command{
		Use:          "upgrade",
		Short:        "Upgrade the embedded cluster operator",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Upgrade command started")

			cli, err := k8sutil.KubeClient()
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			data, err := getInstallationFromSecret(cmd.Context(), cli, secretName, secretNamespace, secretKey)
			if err != nil {
				return fmt.Errorf("get installation from secret: %w", err)
			}

			err = upgrade.Upgrade(cmd.Context(), cli, data)
			if err != nil {
				return fmt.Errorf("failed to upgrade: %w", err)
			}

			fmt.Println("Upgrade command completed successfully")
			return nil
		},
	}

	cmd.Flags().StringVar(&secretName, "installation-secret", "", "The name of the secret containing the installation custom resource")
	err := cmd.MarkFlagRequired("installation-secret")
	if err != nil {
		panic(err)
	}
	cmd.Flags().StringVar(&secretNamespace, "installation-secret-namespace", DefaultInstallationSecretNamespace, "The namespace of the secret containing the installation custom resource")
	cmd.Flags().StringVar(&secretKey, "installation-secret-key", DefaultInstallationSecretKey, "The key in the secret containing the installation custom resource")

	return cmd
}

func getInstallationFromSecret(ctx context.Context, cli client.Client, name, namespace, key string) (*clusterv1beta1.Installation, error) {
	if name == "" {
		return nil, fmt.Errorf("installation secret name is required")
	}
	if namespace == "" {
		namespace = DefaultInstallationSecretNamespace
	}
	if key == "" {
		key = DefaultInstallationSecretKey
	}

	var secret corev1.Secret
	err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &secret)
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	if len(secret.Data[key]) == 0 {
		return nil, fmt.Errorf("key not found in secret")
	}

	decode := serializer.NewCodecFactory(cli.Scheme()).UniversalDeserializer().Decode
	obj, _, err := decode(secret.Data[key], nil, nil)
	if err != nil {
		return nil, fmt.Errorf("decode secret data: %w", err)
	}

	in, ok := obj.(*clusterv1beta1.Installation)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}
	return in, nil
}
