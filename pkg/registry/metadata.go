package registry

import (
	"fmt"

	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"sigs.k8s.io/yaml"
)

func getSeaweedfsS3SecretNameFromHelmExtension(ext k0sv1beta1.HelmExtensions) (string, error) {
	if len(ext.Charts) == 0 {
		return "", fmt.Errorf("chart not found")
	}
	chart := ext.Charts[0]

	var valuesStruct struct {
		Filer struct {
			S3 struct {
				ExistingConfigSecret string `json:"existingConfigSecret"`
			} `json:"s3"`
		} `json:"filer"`
	}
	err := yaml.Unmarshal([]byte(chart.Values), &valuesStruct)
	if err != nil {
		return "", fmt.Errorf("unmarshal chart values: %w", err)
	}
	if valuesStruct.Filer.S3.ExistingConfigSecret == "" {
		return "", fmt.Errorf("secret ref not found")
	}
	return valuesStruct.Filer.S3.ExistingConfigSecret, nil
}

func getRegistryS3SecretNameFromHelmExtension(ext k0sv1beta1.HelmExtensions) (string, error) {
	if len(ext.Charts) == 0 {
		return "", fmt.Errorf("chart not found")
	}
	chart := ext.Charts[0]

	var valuesStruct struct {
		Secrets struct {
			S3 struct {
				SecretRef string `json:"secretRef"`
			} `json:"s3"`
		} `json:"secrets"`
	}
	err := yaml.Unmarshal([]byte(chart.Values), &valuesStruct)
	if err != nil {
		return "", fmt.Errorf("unmarshal chart values: %w", err)
	}
	if valuesStruct.Secrets.S3.SecretRef == "" {
		return "", fmt.Errorf("secret ref not found")
	}
	return valuesStruct.Secrets.S3.SecretRef, nil
}
