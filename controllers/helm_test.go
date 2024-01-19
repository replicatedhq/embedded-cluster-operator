package controllers

import (
	"context"
	"testing"

	"github.com/k0sproject/dig"
	k0shelm "github.com/k0sproject/k0s/pkg/apis/helm/v1beta1"
	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/api/v1beta1"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/release"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	"sigs.k8s.io/yaml"
)

func TestMergeValues(t *testing.T) {
	oldData := `
  password: "foo"
  someField: "asdf"
  other: "text"
  overridden: "abcxyz"
  nested:
    nested:
       protect: "testval"
  `
	newData := `
  someField: "newstring"
  other: "text"
  overridden: "this is new"
  nested:
    nested:
      newkey: "newval"
      protect: "newval"
  `
	protect := []string{"password", "overridden", "nested.nested.protect"}

	targetData := `
  password: "foo"
  someField: "newstring"
  nested:
    nested:
      newkey: "newval"
      protect: "testval"
  other: "text"
  overridden: "abcxyz"
  `

	mergedValues, err := MergeValues(oldData, newData, protect)
	if err != nil {
		t.Fail()
	}

	targetDataMap := dig.Mapping{}
	if err := yaml.Unmarshal([]byte(targetData), &targetDataMap); err != nil {
		t.Fail()
	}

	mergedDataMap := dig.Mapping{}
	if err := yaml.Unmarshal([]byte(mergedValues), &mergedDataMap); err != nil {
		t.Fail()
	}

	assert.DeepEqual(t, targetDataMap, mergedDataMap)

}

func Test_mergeHelmConfigs(t *testing.T) {
	type args struct {
		meta *release.Meta
		in   v1beta1.Extensions
	}
	tests := []struct {
		name string
		args args
		want *k0sv1beta1.HelmExtensions
	}{
		{
			name: "no meta",
			args: args{
				meta: nil,
				in: v1beta1.Extensions{
					Helm: &k0sv1beta1.HelmExtensions{
						ConcurrencyLevel: 2,
						Repositories:     nil,
						Charts: []k0sv1beta1.Chart{
							{
								Name:    "test",
								Version: "1.0.0",
							},
						},
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				ConcurrencyLevel: 1,
				Repositories:     nil,
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
					},
				},
			},
		},
		{
			name: "add new chart + repo",
			args: args{
				meta: &release.Meta{
					Configs: &k0sv1beta1.HelmExtensions{
						ConcurrencyLevel: 1,
						Repositories: []k0sv1beta1.Repository{
							{
								Name: "origrepo",
							},
						},
						Charts: []k0sv1beta1.Chart{
							{
								Name: "origchart",
							},
						},
					},
				},
				in: v1beta1.Extensions{
					Helm: &k0sv1beta1.HelmExtensions{
						Repositories: []k0sv1beta1.Repository{
							{
								Name: "newrepo",
							},
						},
						Charts: []k0sv1beta1.Chart{
							{
								Name:    "newchart",
								Version: "1.0.0",
							},
						},
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				ConcurrencyLevel: 1,
				Repositories: []k0sv1beta1.Repository{
					{
						Name: "origrepo",
					},
					{
						Name: "newrepo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name: "origchart",
					},
					{
						Name:    "newchart",
						Version: "1.0.0",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installation := v1beta1.Installation{
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						Version:    "1.0.0",
						Extensions: tt.args.in,
					},
				},
			}

			req := require.New(t)
			got := mergeHelmConfigs(tt.args.meta, &installation)
			req.Equal(tt.want, got)
		})
	}
}

func Test_detectChartDrift(t *testing.T) {
	type args struct {
		combinedConfigs *k0sv1beta1.HelmExtensions
		installedCharts k0shelm.ChartList
	}
	tests := []struct {
		name            string
		args            args
		wantChartErrors []string
		wantDrift       bool
	}{
		{
			name: "no drift",
			args: args{
				combinedConfigs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "test",
							Version: "1.0.0",
						},
						{
							Name:    "test2",
							Version: "2.0.0",
						},
					},
				},
				installedCharts: k0shelm.ChartList{
					Items: []k0shelm.Chart{
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test",
							},
							Spec: k0shelm.ChartSpec{
								Version: "1.0.0",
							},
						},
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test2",
							},
							Spec: k0shelm.ChartSpec{
								Version: "2.0.0",
							},
						},
					},
				},
			},
			wantDrift:       false,
			wantChartErrors: []string{},
		},
		{
			name: "new chart",
			args: args{
				combinedConfigs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "test",
							Version: "1.0.0",
						},
						{
							Name:    "test2",
							Version: "2.0.0",
						},
					},
				},
				installedCharts: k0shelm.ChartList{
					Items: []k0shelm.Chart{
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test",
							},
							Spec: k0shelm.ChartSpec{
								Version: "1.0.0",
							},
						},
					},
				},
			},
			wantDrift:       true,
			wantChartErrors: []string{},
		},
		{
			name: "removed chart",
			args: args{
				combinedConfigs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "test",
							Version: "1.0.0",
						},
					},
				},
				installedCharts: k0shelm.ChartList{
					Items: []k0shelm.Chart{
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test",
							},
							Spec: k0shelm.ChartSpec{
								Version: "1.0.0",
							},
						},
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test2",
							},
							Spec: k0shelm.ChartSpec{
								Version: "2.0.0",
							},
						},
					},
				},
			},
			wantDrift:       true,
			wantChartErrors: []string{},
		},
		{
			name: "added and removed chart",
			args: args{
				combinedConfigs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "test2",
							Version: "2.0.0",
						},
					},
				},
				installedCharts: k0shelm.ChartList{
					Items: []k0shelm.Chart{
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test",
							},
							Spec: k0shelm.ChartSpec{
								Version: "1.0.0",
							},
						},
					},
				},
			},
			wantDrift:       true,
			wantChartErrors: []string{},
		},
		{
			name: "no drift, but error",
			args: args{
				combinedConfigs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "test",
							Version: "1.0.0",
						},
						{
							Name:    "test2",
							Version: "2.0.0",
						},
					},
				},
				installedCharts: k0shelm.ChartList{
					Items: []k0shelm.Chart{
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test",
								Error:       "test chart error",
							},
							Spec: k0shelm.ChartSpec{
								Version: "1.0.0",
							},
						},
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test2",
								Error:       "test chart two error",
							},
							Spec: k0shelm.ChartSpec{
								Version: "2.0.0",
							},
						},
					},
				},
			},
			wantDrift:       false,
			wantChartErrors: []string{"test chart error", "test chart two error"},
		},
		{
			name: "drift and error",
			args: args{
				combinedConfigs: &k0sv1beta1.HelmExtensions{
					Charts: []k0sv1beta1.Chart{
						{
							Name:    "test",
							Version: "1.0.0",
						},
						{
							Name:    "test2",
							Version: "2.0.1",
						},
					},
				},
				installedCharts: k0shelm.ChartList{
					Items: []k0shelm.Chart{
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test",
								Error:       "test chart error",
							},
							Spec: k0shelm.ChartSpec{
								Version: "1.0.0",
							},
						},
						{
							Status: k0shelm.ChartStatus{
								ReleaseName: "test2",
								Error:       "test chart two error",
							},
							Spec: k0shelm.ChartSpec{
								Version: "2.0.0",
							},
						},
					},
				},
			},
			wantDrift:       true,
			wantChartErrors: []string{"test chart error", "test chart two error"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := require.New(t)

			ctx := context.Background()
			gotErrors, gotDrift := detectChartDrift(ctx, tt.args.combinedConfigs, tt.args.installedCharts)
			req.Equal(tt.wantChartErrors, gotErrors)
			req.Equal(tt.wantDrift, gotDrift)
		})
	}
}
