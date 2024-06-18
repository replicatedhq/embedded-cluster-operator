package artifacts

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	clusterv1beta1 "github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/k8sutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestEnsureArtifactsJobForNodes(t *testing.T) {
	type args struct {
		in                       *clusterv1beta1.Installation
		localArtifactMirrorImage string
	}
	tests := []struct {
		name            string
		initRuntimeObjs []client.Object
		args            args
		wantErr         bool
		assertRuntime   func(t *testing.T, cli client.Client, in *clusterv1beta1.Installation)
	}{
		{
			name: "create artifacts job",
			initRuntimeObjs: []client.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ecNamespace,
					},
				},
			},
			args: args{
				in: &clusterv1beta1.Installation{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-installation",
					},
					Spec: clusterv1beta1.InstallationSpec{
						Artifacts: &clusterv1beta1.ArtifactsLocation{
							Images:                  "images",
							HelmCharts:              "helm-charts",
							EmbeddedClusterBinary:   "embedded-cluster-binary",
							EmbeddedClusterMetadata: "embedded-cluster-metadata",
						},
					},
				},
				localArtifactMirrorImage: "local-artifact-mirror",
			},
			wantErr: false,
			assertRuntime: func(t *testing.T, cli client.Client, in *clusterv1beta1.Installation) {
				artifactsHash, err := HashForAirgapConfig(in)
				require.NoError(t, err)

				job := &batchv1.Job{}

				err = cli.Get(context.Background(), client.ObjectKey{Namespace: ecNamespace, Name: copyArtifactsJobPrefix + "node1"}, job)
				require.NoError(t, err)

				assert.Equal(t, "test-installation", job.ObjectMeta.Annotations[InstallationNameAnnotation])
				assert.Equal(t, artifactsHash, job.ObjectMeta.Annotations[ArtifactsConfigHashAnnotation])
				assert.Equal(t, "local-artifact-mirror", job.Spec.Template.Spec.Containers[0].Image)

				err = cli.Get(context.Background(), client.ObjectKey{Namespace: ecNamespace, Name: copyArtifactsJobPrefix + "node2"}, job)
				require.NoError(t, err)

				assert.Equal(t, "test-installation", job.ObjectMeta.Annotations[InstallationNameAnnotation])
				assert.Equal(t, artifactsHash, job.ObjectMeta.Annotations[ArtifactsConfigHashAnnotation])
				assert.Equal(t, "local-artifact-mirror", job.Spec.Template.Spec.Containers[0].Image)
			},
		},
		{
			name: "replace existing artifacts job",
			initRuntimeObjs: []client.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ecNamespace,
					},
				},
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ecNamespace,
						Name:      copyArtifactsJobPrefix + "node1",
						Annotations: map[string]string{
							InstallationNameAnnotation:    "test-installation",
							ArtifactsConfigHashAnnotation: "old-hash",
						},
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:  "copy-artifacts",
										Image: "old-image",
									},
								},
							},
						},
					},
				},
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ecNamespace,
						Name:      copyArtifactsJobPrefix + "node2",
						Annotations: map[string]string{
							InstallationNameAnnotation:    "old-installation",
							ArtifactsConfigHashAnnotation: "old-hash",
						},
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:  "copy-artifacts",
										Image: "old-image",
									},
								},
							},
						},
					},
				},
			},
			args: args{
				in: &clusterv1beta1.Installation{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-installation",
					},
					Spec: clusterv1beta1.InstallationSpec{
						Artifacts: &clusterv1beta1.ArtifactsLocation{
							Images:                  "images",
							HelmCharts:              "helm-charts",
							EmbeddedClusterBinary:   "embedded-cluster-binary",
							EmbeddedClusterMetadata: "embedded-cluster-metadata",
						},
					},
				},
				localArtifactMirrorImage: "local-artifact-mirror",
			},
			wantErr: false,
			assertRuntime: func(t *testing.T, cli client.Client, in *clusterv1beta1.Installation) {
				artifactsHash, err := HashForAirgapConfig(in)
				require.NoError(t, err)

				job := &batchv1.Job{}

				err = cli.Get(context.Background(), client.ObjectKey{Namespace: ecNamespace, Name: copyArtifactsJobPrefix + "node1"}, job)
				require.NoError(t, err)

				assert.Equal(t, "test-installation", job.ObjectMeta.Annotations[InstallationNameAnnotation])
				assert.Equal(t, artifactsHash, job.ObjectMeta.Annotations[ArtifactsConfigHashAnnotation])
				assert.Equal(t, "local-artifact-mirror", job.Spec.Template.Spec.Containers[0].Image)

				err = cli.Get(context.Background(), client.ObjectKey{Namespace: ecNamespace, Name: copyArtifactsJobPrefix + "node2"}, job)
				require.NoError(t, err)

				assert.Equal(t, "test-installation", job.ObjectMeta.Annotations[InstallationNameAnnotation])
				assert.Equal(t, artifactsHash, job.ObjectMeta.Annotations[ArtifactsConfigHashAnnotation])
				assert.Equal(t, "local-artifact-mirror", job.Spec.Template.Spec.Containers[0].Image)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
			ctx := logr.NewContext(context.Background(), log)

			testEnv := &envtest.Environment{}
			cfg, err := testEnv.Start()
			require.NoError(t, err)
			t.Cleanup(func() { _ = testEnv.Stop() })

			cli, err := client.New(cfg, client.Options{Scheme: k8sutil.Scheme()})
			require.NoError(t, err)

			for _, obj := range tt.initRuntimeObjs {
				err := cli.Create(ctx, obj)
				require.NoError(t, err)
			}

			// there is no job controller in envtest so the finalizer will not be removed
			ensureArtifactsJobForNodeDeletePropagation = metav1.DeletePropagationBackground
			t.Cleanup(func() { ensureArtifactsJobForNodeDeletePropagation = metav1.DeletePropagationForeground })

			if err := EnsureArtifactsJobForNodes(ctx, cli, tt.args.in, tt.args.localArtifactMirrorImage); (err != nil) != tt.wantErr {
				t.Errorf("EnsureArtifactsJobForNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			tt.assertRuntime(t, cli, tt.args.in)
		})
	}
}

func TestListArtifactsJobForNodes(t *testing.T) {
	in := &clusterv1beta1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-installation",
		},
		Spec: clusterv1beta1.InstallationSpec{
			Artifacts: &clusterv1beta1.ArtifactsLocation{
				Images:                  "images",
				HelmCharts:              "helm-charts",
				EmbeddedClusterBinary:   "embedded-cluster-binary",
				EmbeddedClusterMetadata: "embedded-cluster-metadata",
			},
		},
	}

	type args struct {
		in *clusterv1beta1.Installation
	}
	tests := []struct {
		name            string
		initRuntimeObjs []client.Object
		args            args
		wantErr         bool
		assertWant      func(t *testing.T, in *clusterv1beta1.Installation, want map[string]*batchv1.Job)
	}{
		{
			name: "list artifacts job",
			initRuntimeObjs: []client.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ecNamespace,
					},
				},
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ecNamespace,
						Name:      copyArtifactsJobPrefix + "node1",
						Annotations: map[string]string{
							InstallationNameAnnotation: "test-installation",
							ArtifactsConfigHashAnnotation: func() string {
								artifactsHash, err := HashForAirgapConfig(in)
								require.NoError(t, err)
								return artifactsHash
							}(),
						},
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:  "copy-artifacts",
										Image: "image",
									},
								},
							},
						},
					},
				},
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ecNamespace,
						Name:      copyArtifactsJobPrefix + "node2",
						Annotations: map[string]string{
							InstallationNameAnnotation:    "test-installation",
							ArtifactsConfigHashAnnotation: "old-hash",
						},
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:  "copy-artifacts",
										Image: "image",
									},
								},
							},
						},
					},
				},
			},
			args: args{
				in: in,
			},
			wantErr: false,
			assertWant: func(t *testing.T, in *clusterv1beta1.Installation, want map[string]*batchv1.Job) {
				artifactsHash, err := HashForAirgapConfig(in)
				require.NoError(t, err)

				assert.Len(t, want, 3)

				job := want["node1"]
				assert.Equal(t, "test-installation", job.ObjectMeta.Annotations[InstallationNameAnnotation])
				assert.Equal(t, artifactsHash, job.ObjectMeta.Annotations[ArtifactsConfigHashAnnotation])

				// old hash
				job = want["node2"]
				assert.Nil(t, job)

				// missing
				job = want["node3"]
				assert.Nil(t, job)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
			ctx := logr.NewContext(context.Background(), log)

			testEnv := &envtest.Environment{}
			cfg, err := testEnv.Start()
			require.NoError(t, err)
			t.Cleanup(func() { _ = testEnv.Stop() })

			cli, err := client.New(cfg, client.Options{Scheme: k8sutil.Scheme()})
			require.NoError(t, err)

			for _, obj := range tt.initRuntimeObjs {
				err := cli.Create(ctx, obj)
				require.NoError(t, err)
			}

			got, err := ListArtifactsJobForNodes(ctx, cli, tt.args.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListArtifactsJobForNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.assertWant != nil {
				tt.assertWant(t, tt.args.in, got)
			}
		})
	}
}
