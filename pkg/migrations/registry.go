package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/registry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// registryData copies data from the disk (/var/lib/embedded-cluster/registry) to the seaweedfs s3 store.
// if it fails, it will scale the registry deployment back to 1.
// if it succeeds, it will create a secret used to indicate success to the operator.
func registryData() error {
	// if the migration fails, we need to scale the registry back to 1
	success := false
	defer func() {
		if !success {
			err := registryScale(1)
			if err != nil {
				fmt.Printf("Failed to scale registry back to 1 replica: %v\n", err)
			}
		}
	}()
	err := registryScale(0)
	if err != nil {
		return fmt.Errorf("failed to scale registry to 0 replicas before uploading data: %w", err)
	}

	fmt.Printf("Connecting to s3\n")
	creds := credentials.NewStaticCredentialsProvider(os.Getenv("s3AccessKey"), os.Getenv("s3SecretKey"), "")
	conf, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(creds),
		config.WithRegion("us-east-1"),
		config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: "http://seaweedfs-s3.seaweedfs:8333"}, nil
			})),
	)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	s3Client := s3.New(s3.Options{Credentials: creds, UsePathStyle: true})
	registryStr := "registry"
	_, err = s3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
		Bucket: &registryStr,
	})
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}

	fmt.Printf("Running registry data migration\n")
	err = filepath.Walk("/var/lib/embedded-cluster/registry", func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		relPath, err := filepath.Rel("/var/lib/embedded-cluster/registry", path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}

		fmt.Printf("uploading %s, size %d\n", relPath, info.Size())
		_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket: &registryStr,
			Body:   f,
			Key:    &relPath,
		})
		if err != nil {
			return fmt.Errorf("upload object: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk registry data: %w", err)
	}

	fmt.Printf("Creating registry data migration secret\n")
	cli, err := kubeClient()
	if err != nil {
		return fmt.Errorf("unable to create kubernetes client: %w", err)
	}

	migrationSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      registry.RegistryDataMigrationCompleteSecretName,
			Namespace: registry.RegistryNamespace(),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		Data: map[string][]byte{
			"migration": []byte("complete"),
		},
	}
	err = cli.Create(context.TODO(), &migrationSecret)
	if err != nil {
		return fmt.Errorf("create registry data migration secret: %w", err)
	}

	success = true
	fmt.Printf("Registry data migration complete\n")
	return nil
}

// registryScale scales the registry deployment to the given replica count.
// '0' and '1' are the only acceptable values.
func registryScale(scale int32) error {
	if scale != 0 && scale != 1 {
		return fmt.Errorf("invalid scale: %d", scale)
	}

	cli, err := kubeClient()
	if err != nil {
		return fmt.Errorf("unable to create kubernetes client: %w", err)
	}

	fmt.Printf("Finding current registry deployment\n")

	currentRegistry := &appsv1.Deployment{}
	err = cli.Get(context.TODO(), client.ObjectKey{Namespace: registry.RegistryNamespace(), Name: "registry"}, currentRegistry)
	if err != nil {
		return fmt.Errorf("get registry deployment: %w", err)
	}

	fmt.Printf("Scaling registry to %d replicas\n", scale)

	currentRegistry.Spec.Replicas = &scale

	err = cli.Update(context.TODO(), currentRegistry)
	if err != nil {
		return fmt.Errorf("update registry deployment: %w", err)
	}

	fmt.Printf("Registry scaled to %d replicas\n", scale)

	return nil
}
