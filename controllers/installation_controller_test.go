package controllers

import (
	"context"
	k0shelmv1beta1 "github.com/k0sproject/k0s/pkg/apis/helm/v1beta1"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/api/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestInstallationReconciler_ReconcileHelmCharts(t *testing.T) {
	type fields struct {
		State     []runtime.Object
		Discovery discovery.DiscoveryInterface
		Scheme    *runtime.Scheme
	}
	tests := []struct {
		name        string
		fields      fields
		in          v1beta1.Installation
		out         v1beta1.InstallationStatus
		releaseMeta release.Meta
	}{
		{
			name: "no input config, move to installed",
			in: v1beta1.Installation{
				Status: v1beta1.InstallationStatus{State: v1beta1.InstallationStateKubernetesInstalled},
			},
			out: v1beta1.InstallationStatus{State: v1beta1.InstallationStateInstalled, Reason: "Installed"},
		},
		{
			name: "k8s install in progress, no state change",
			in: v1beta1.Installation{
				Status: v1beta1.InstallationStatus{State: v1beta1.InstallationStateInstalling},
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						Version: "abc",
					},
				},
			},
			out: v1beta1.InstallationStatus{State: v1beta1.InstallationStateInstalling},
		},
		{
			name: "k8s install completed, good version, no charts",
			in: v1beta1.Installation{
				Status: v1beta1.InstallationStatus{State: v1beta1.InstallationStateKubernetesInstalled},
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						Version: "goodver",
					},
				},
			},
			out: v1beta1.InstallationStatus{
				State:  v1beta1.InstallationStateInstalled,
				Reason: "Installed",
			},
			releaseMeta: release.Meta{K0sSHA: "abc"},
		},
		{
			name: "k8s install completed, good version, both types of charts, no drift",
			in: v1beta1.Installation{
				Status: v1beta1.InstallationStatus{State: v1beta1.InstallationStateKubernetesInstalled},
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						Version: "goodver",
						Extensions: v1beta1.Extensions{
							Helm: &k0sv1beta1.HelmExtensions{
								Charts: []k0sv1beta1.Chart{
									{
										Name:    "extchart",
										Version: "2",
									},
								},
							},
						},
					},
				},
			},
			out: v1beta1.InstallationStatus{
				State:  v1beta1.InstallationStateInstalled,
				Reason: "Addons upgraded",
			},
			releaseMeta: release.Meta{
				Configs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "metachart",
							Version: "1",
						},
					},
				},
			},
			fields: fields{
				State: []runtime.Object{
					&k0shelmv1beta1.Chart{
						ObjectMeta: metav1.ObjectMeta{
							Name: "metachart",
						},
						Spec:   k0shelmv1beta1.ChartSpec{ChartName: "metachart"},
						Status: k0shelmv1beta1.ChartStatus{Version: "1"},
					},
					&k0shelmv1beta1.Chart{
						ObjectMeta: metav1.ObjectMeta{
							Name: "extchart",
						},
						Spec:   k0shelmv1beta1.ChartSpec{ChartName: "extchart"},
						Status: k0shelmv1beta1.ChartStatus{Version: "2"},
					},
				},
			},
		},
	}
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			req := require.New(t)

			release.CacheMeta("goodver", tt.releaseMeta)

			sch, err := k0shelmv1beta1.SchemeBuilder.Build()
			req.NoError(err)
			fakeCli := fake.NewClientBuilder().WithScheme(sch).WithRuntimeObjects(tt.fields.State...).Build()

			r := &InstallationReconciler{
				Client:    fakeCli,
				Discovery: tt.fields.Discovery,
				Scheme:    tt.fields.Scheme,
			}
			err = r.ReconcileHelmCharts(context.Background(), &tt.in)
			req.NoError(err)
			req.Equal(tt.out, tt.in.Status)
		})
	}
}
