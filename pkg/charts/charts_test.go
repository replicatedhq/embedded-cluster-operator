package charts

import (
	"context"
	"testing"

	k0sv1beta1 "github.com/k0sproject/k0s/pkg/apis/k0s/v1beta1"
	"github.com/replicatedhq/embedded-cluster-kinds/apis/v1beta1"
	ectypes "github.com/replicatedhq/embedded-cluster-kinds/types"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/registry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_mergeHelmConfigs(t *testing.T) {
	type args struct {
		meta          *ectypes.ReleaseMetadata
		in            v1beta1.Extensions
		conditions    []metav1.Condition
		clusterConfig k0sv1beta1.ClusterConfig
	}
	tests := []struct {
		name             string
		args             args
		airgap           bool
		highAvailability bool
		disasterRecovery bool
		want             *k0sv1beta1.HelmExtensions
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
								Order:   2,
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
						Order:   102,
					},
				},
			},
		},
		{
			name: "add new chart + repo",
			args: args{
				meta: &ectypes.ReleaseMetadata{
					Configs: k0sv1beta1.HelmExtensions{
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
						Name:  "origchart",
						Order: 110,
					},
					{
						Name:    "newchart",
						Version: "1.0.0",
						Order:   110,
					},
				},
			},
		},
		{
			name:             "disaster recovery enabled",
			disasterRecovery: true,
			args: args{
				meta: &ectypes.ReleaseMetadata{
					Configs: k0sv1beta1.HelmExtensions{
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
					BuiltinConfigs: map[string]k0sv1beta1.HelmExtensions{
						"velero": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "velerorepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "velerochart",
								},
							},
						},
					},
				},
				in: v1beta1.Extensions{},
			},
			want: &k0sv1beta1.HelmExtensions{
				ConcurrencyLevel: 1,
				Repositories: []k0sv1beta1.Repository{
					{
						Name: "origrepo",
					},
					{
						Name: "velerorepo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name:  "origchart",
						Order: 100,
					},
					{
						Name:  "velerochart",
						Order: 100,
					},
				},
			},
		},
		{
			name:   "airgap enabled",
			airgap: true,
			args: args{
				meta: &ectypes.ReleaseMetadata{
					Configs: k0sv1beta1.HelmExtensions{
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
					BuiltinConfigs: map[string]k0sv1beta1.HelmExtensions{
						"seaweedfs": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "seaweedfsrepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "seaweedfschart",
									// Values: `{"filer":{"s3":{"existingConfigSecret":"seaweedfs-s3-secret"}}}`,
								},
							},
						},
						"registry": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "registryrepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "registrychart",
								},
							},
						},
						"registry-ha": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "registryharepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "registryhachart",
									// Values: `{"secrets":{"s3":{"secretRef":"registry-s3-secret"}}}`,
								},
							},
						},
					},
				},
				in: v1beta1.Extensions{},
			},
			want: &k0sv1beta1.HelmExtensions{
				ConcurrencyLevel: 1,
				Repositories: []k0sv1beta1.Repository{
					{
						Name: "origrepo",
					},
					{
						Name: "registryrepo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name:  "origchart",
						Order: 100,
					},
					{
						Name:  "registrychart",
						Order: 100,
					},
				},
			},
		},
		{
			name:             "ha airgap enabled",
			airgap:           true,
			highAvailability: true,
			args: args{
				meta: &ectypes.ReleaseMetadata{
					Configs: k0sv1beta1.HelmExtensions{
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
					BuiltinConfigs: map[string]k0sv1beta1.HelmExtensions{
						"seaweedfs": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "seaweedfsrepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "seaweedfschart",
									// Values: `{"filer":{"s3":{"existingConfigSecret":"seaweedfs-s3-secret"}}}`,
								},
							},
						},
						"registry": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "registryrepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "registrychart",
								},
							},
						},
						"registry-ha": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "registryharepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "registryhachart",
									// Values: `{"secrets":{"s3":{"secretRef":"registry-s3-secret"}}}`,
								},
							},
						},
					},
				},
				in: v1beta1.Extensions{},
				conditions: []metav1.Condition{
					{
						Type:   registry.RegistryMigrationStatusConditionType,
						Status: metav1.ConditionTrue,
						Reason: "MigrationJobCompleted",
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
						Name: "seaweedfsrepo",
					},
					{
						Name: "registryharepo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name:  "origchart",
						Order: 100,
					},
					{
						Name:  "seaweedfschart",
						Order: 100,
					},
					{
						Name:  "registryhachart",
						Order: 100,
					},
				},
			},
		},
		{
			name:             "ha airgap enabled, migration incomplete",
			airgap:           true,
			highAvailability: true,
			args: args{
				meta: &ectypes.ReleaseMetadata{
					Configs: k0sv1beta1.HelmExtensions{
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
					BuiltinConfigs: map[string]k0sv1beta1.HelmExtensions{
						"seaweedfs": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "seaweedfsrepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "seaweedfschart",
									// Values: `{"filer":{"s3":{"existingConfigSecret":"seaweedfs-s3-secret"}}}`,
								},
							},
						},
						"registry": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "registryrepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "registrychart",
								},
							},
						},
						"registry-ha": {
							Repositories: []k0sv1beta1.Repository{
								{
									Name: "registryharepo",
								},
							},
							Charts: []k0sv1beta1.Chart{
								{
									Name: "registryhachart",
									// Values: `{"secrets":{"s3":{"secretRef":"registry-s3-secret"}}}`,
								},
							},
						},
					},
				},
				in: v1beta1.Extensions{},
				conditions: []metav1.Condition{
					{
						Type:   registry.RegistryMigrationStatusConditionType,
						Status: metav1.ConditionFalse,
						Reason: "MigrationInProgress",
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
						Name: "seaweedfsrepo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name:  "origchart",
						Order: 100,
					},
					{
						Name:  "seaweedfschart",
						Order: 100,
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
					AirGap:           tt.airgap,
					HighAvailability: tt.highAvailability,
					LicenseInfo: &v1beta1.LicenseInfo{
						IsDisasterRecoverySupported: tt.disasterRecovery,
					},
				},
				Status: v1beta1.InstallationStatus{
					Conditions: tt.args.conditions,
				},
			}

			req := require.New(t)
			got, err := mergeHelmConfigs(context.TODO(), tt.args.meta, &installation, &tt.args.clusterConfig)
			req.NoError(err)
			req.Equal(tt.want, got)
		})
	}
}

func Test_applyUserProvidedAddonOverrides(t *testing.T) {
	tests := []struct {
		name         string
		installation *v1beta1.Installation
		config       *k0sv1beta1.HelmExtensions
		want         *k0sv1beta1.HelmExtensions
	}{
		{
			name:         "no config",
			installation: &v1beta1.Installation{},
			config: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
				},
			},
		},
		{
			name: "no override",
			installation: &v1beta1.Installation{
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						UnsupportedOverrides: v1beta1.UnsupportedOverrides{
							BuiltInExtensions: []v1beta1.BuiltInExtension{},
						},
					},
				},
			},
			config: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
				},
			},
		},
		{
			name: "single addition",
			installation: &v1beta1.Installation{
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						UnsupportedOverrides: v1beta1.UnsupportedOverrides{
							BuiltInExtensions: []v1beta1.BuiltInExtension{
								{
									Name:   "test",
									Values: "foo: bar",
								},
							},
						},
					},
				},
			},
			config: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz\nfoo: bar\n",
					},
				},
			},
		},
		{
			name: "single override",
			installation: &v1beta1.Installation{
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						UnsupportedOverrides: v1beta1.UnsupportedOverrides{
							BuiltInExtensions: []v1beta1.BuiltInExtension{
								{
									Name:   "test",
									Values: "abc: newvalue",
								},
							},
						},
					},
				},
			},
			config: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "test",
						Version: "1.0.0",
						Values:  "abc: newvalue\n",
					},
				},
			},
		},
		{
			name: "multiple additions and overrides",
			installation: &v1beta1.Installation{
				Spec: v1beta1.InstallationSpec{
					Config: &v1beta1.ConfigSpec{
						UnsupportedOverrides: v1beta1.UnsupportedOverrides{
							BuiltInExtensions: []v1beta1.BuiltInExtension{
								{
									Name:   "chart0",
									Values: "added: added\noverridden: overridden",
								},
								{
									Name:   "chart1",
									Values: "foo: replacement",
								},
							},
						},
					},
				},
			},
			config: &k0sv1beta1.HelmExtensions{
				ConcurrencyLevel: 999,
				Repositories: []k0sv1beta1.Repository{
					{
						Name: "repo",
						URL:  "https://repo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "chart0",
						Version: "1.0.0",
						Values:  "abc: xyz",
					},
					{
						Name:    "chart1",
						Version: "1.0.0",
						Values:  "foo: bar",
					},
				},
			},
			want: &k0sv1beta1.HelmExtensions{
				ConcurrencyLevel: 999,
				Repositories: []k0sv1beta1.Repository{
					{
						Name: "repo",
						URL:  "https://repo",
					},
				},
				Charts: []k0sv1beta1.Chart{
					{
						Name:    "chart0",
						Version: "1.0.0",
						Values:  "abc: xyz\nadded: added\noverridden: overridden\n",
					},
					{
						Name:    "chart1",
						Version: "1.0.0",
						Values:  "foo: replacement\n",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := require.New(t)
			got, err := applyUserProvidedAddonOverrides(tt.installation, tt.config)
			req.NoError(err)
			req.Equal(tt.want, got)
		})
	}
}

func Test_updateInfraChartsFromInstall(t *testing.T) {
	type args struct {
		in            *v1beta1.Installation
		clusterConfig k0sv1beta1.ClusterConfig
		charts        []k0sv1beta1.Chart
	}
	tests := []struct {
		name string
		args args
		want []k0sv1beta1.Chart
	}{
		{
			name: "other chart",
			args: args{
				in: &v1beta1.Installation{
					Spec: v1beta1.InstallationSpec{
						ClusterID: "abc",
					},
				},
				charts: []k0sv1beta1.Chart{
					{
						Name:   "test",
						Values: "abc: xyz",
					},
				},
			},
			want: []k0sv1beta1.Chart{
				{
					Name:   "test",
					Values: "abc: xyz",
				},
			},
		},
		{
			name: "admin console and operator",
			args: args{
				in: &v1beta1.Installation{
					Spec: v1beta1.InstallationSpec{
						ClusterID:        "testid",
						BinaryName:       "testbin",
						AirGap:           true,
						HighAvailability: true,
					},
				},
				charts: []k0sv1beta1.Chart{
					{
						Name:   "test",
						Values: "abc: xyz",
					},
					{
						Name:   "admin-console",
						Values: "abc: xyz",
					},
					{
						Name:   "embedded-cluster-operator",
						Values: "this: that",
					},
				},
			},
			want: []k0sv1beta1.Chart{
				{
					Name:   "test",
					Values: "abc: xyz",
				},
				{
					Name:   "admin-console",
					Values: "abc: xyz\nembeddedClusterID: testid\nisAirgap: \"true\"\nisHA: true\n",
				},
				{
					Name:   "embedded-cluster-operator",
					Values: "embeddedBinaryName: testbin\nembeddedClusterID: testid\nthis: that\n",
				},
			},
		},
		{
			name: "admin console and operator with proxy",
			args: args{
				in: &v1beta1.Installation{
					Spec: v1beta1.InstallationSpec{
						ClusterID:        "testid",
						BinaryName:       "testbin",
						AirGap:           false,
						HighAvailability: false,
						Proxy: &v1beta1.ProxySpec{
							HTTPProxy:  "http://proxy",
							HTTPSProxy: "https://proxy",
							NoProxy:    "noproxy",
						},
					},
				},
				charts: []k0sv1beta1.Chart{
					{
						Name:   "test",
						Values: "abc: xyz",
					},
					{
						Name:   "admin-console",
						Values: "abc: xyz",
					},
					{
						Name:   "embedded-cluster-operator",
						Values: "this: that",
					},
				},
			},
			want: []k0sv1beta1.Chart{
				{
					Name:   "test",
					Values: "abc: xyz",
				},
				{
					Name:   "admin-console",
					Values: "abc: xyz\nembeddedClusterID: testid\nextraEnv:\n- name: HTTP_PROXY\n  value: http://proxy\n- name: HTTPS_PROXY\n  value: https://proxy\n- name: NO_PROXY\n  value: noproxy\nisAirgap: \"false\"\nisHA: false\n",
				},
				{
					Name:   "embedded-cluster-operator",
					Values: "embeddedBinaryName: testbin\nembeddedClusterID: testid\nextraEnv:\n- name: HTTP_PROXY\n  value: http://proxy\n- name: HTTPS_PROXY\n  value: https://proxy\n- name: NO_PROXY\n  value: noproxy\nthis: that\n",
				},
			},
		},
		{
			name: "velero with proxy",
			args: args{
				in: &v1beta1.Installation{
					Spec: v1beta1.InstallationSpec{
						ClusterID:        "testid",
						BinaryName:       "testbin",
						AirGap:           false,
						HighAvailability: false,
						Proxy: &v1beta1.ProxySpec{
							HTTPProxy:  "http://proxy",
							HTTPSProxy: "https://proxy",
							NoProxy:    "noproxy",
						},
					},
				},
				charts: []k0sv1beta1.Chart{
					{
						Name:   "velero",
						Values: "abc: xyz\nconfiguration:\n  extraEnvVars: {}\n",
					},
				},
			},
			want: []k0sv1beta1.Chart{
				{
					Name:   "velero",
					Values: "abc: xyz\nconfiguration:\n  extraEnvVars:\n    HTTP_PROXY: http://proxy\n    HTTPS_PROXY: https://proxy\n    NO_PROXY: noproxy\n",
				},
			},
		},
		{
			name: "docker-registry",
			args: args{
				in: &v1beta1.Installation{
					Spec: v1beta1.InstallationSpec{
						ClusterID:  "testid",
						BinaryName: "testbin",
						AirGap:     true,
					},
				},
				clusterConfig: k0sv1beta1.ClusterConfig{},
				charts: []k0sv1beta1.Chart{
					{
						Name:   "docker-registry",
						Values: "this: that\nand: another\n",
					},
				},
			},
			want: []k0sv1beta1.Chart{
				{
					Name:   "docker-registry",
					Values: "this: that\nand: another\n",
				},
			},
		},
		{
			name: "docker-registry ha",
			args: args{
				in: &v1beta1.Installation{
					Spec: v1beta1.InstallationSpec{
						ClusterID:        "testid",
						BinaryName:       "testbin",
						AirGap:           true,
						HighAvailability: true,
					},
				},
				clusterConfig: k0sv1beta1.ClusterConfig{},
				charts: []k0sv1beta1.Chart{
					{
						Name: "docker-registry",
						Values: `image:
  tag: 2.8.3
replicaCount: 2
s3:
  bucket: registry
  encrypt: false
  region: us-east-1
  regionEndpoint: DYNAMIC
  rootdirectory: /registry
  secure: false
secrets:
  s3:
    secretRef: seaweedfs-s3-rw`,
					},
				},
			},
			want: []k0sv1beta1.Chart{
				{
					Name: "docker-registry",
					Values: `image:
  tag: 2.8.3
replicaCount: 2
s3:
  bucket: registry
  encrypt: false
  region: us-east-1
  regionEndpoint: 10.96.0.12:8333
  rootdirectory: /registry
  secure: false
secrets:
  s3:
    secretRef: seaweedfs-s3-rw
`,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := require.New(t)
			got, err := updateInfraChartsFromInstall(tt.args.in, &tt.args.clusterConfig, tt.args.charts)
			req.NoError(err)
			req.ElementsMatch(tt.want, got)
		})
	}
}
