package manifests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/installer/pkg/asset"
	"github.com/openshift/installer/pkg/asset/agent"
	"github.com/openshift/installer/pkg/asset/agent/agentconfig"
	"github.com/openshift/installer/pkg/types"
)

var (
	infraEnvFilename = filepath.Join(clusterManifestDir, "infraenv.yaml")
)

// InfraEnv generates the infraenv.yaml file.
type InfraEnv struct {
	File   *asset.File
	Config *aiv1beta1.InfraEnv
}

var _ asset.WritableAsset = (*InfraEnv)(nil)

// Name returns a human friendly name for the asset.
func (*InfraEnv) Name() string {
	return "InfraEnv Config"
}

// Dependencies returns all of the dependencies directly needed to generate
// the asset.
func (*InfraEnv) Dependencies() []asset.Asset {
	return []asset.Asset{
		&agent.OptionalInstallConfig{},
		&agentconfig.AgentConfig{},
	}
}

// Generate generates the InfraEnv manifest.
func (i *InfraEnv) Generate(dependencies asset.Parents) error {

	installConfig := &agent.OptionalInstallConfig{}
	agentConfig := &agentconfig.AgentConfig{}
	dependencies.Get(installConfig, agentConfig)

	if installConfig.Config != nil {
		infraEnv := &aiv1beta1.InfraEnv{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getInfraEnvName(installConfig),
				Namespace: getObjectMetaNamespace(installConfig),
			},
			Spec: aiv1beta1.InfraEnvSpec{
				ClusterRef: &aiv1beta1.ClusterReference{
					Name:      getClusterDeploymentName(installConfig),
					Namespace: getObjectMetaNamespace(installConfig),
				},
				SSHAuthorizedKey: strings.Trim(installConfig.Config.SSHKey, "|\n\t"),
				PullSecretRef: &corev1.LocalObjectReference{
					Name: getPullSecretName(installConfig),
				},
				NMStateConfigLabelSelector: metav1.LabelSelector{
					MatchLabels: getNMStateConfigLabels(installConfig),
				},
			},
		}

		// Use installConfig.Config.ControlPlane.Architecture to determine cpuarchitecture for infraEnv.Spec.CpuArchiteture.
		// installConfig.Config.ControlPlance.Architecture uses go/Debian cpuarchitecture values (amd64, arm64) so we must convert to rpmArch because infraEnv.Spec.CpuArchitecture expects x86_64 or aarch64.
		if installConfig.Config.ControlPlane.Architecture != "" {
			infraEnv.Spec.CpuArchitecture = arch.RpmArch(string(installConfig.Config.ControlPlane.Architecture))
		}
		if installConfig.Config.Proxy != nil {
			infraEnv.Spec.Proxy = getProxy(installConfig)
		}

		if agentConfig.Config != nil {
			infraEnv.Spec.AdditionalNTPSources = agentConfig.Config.AdditionalNTPSources
		}
		i.Config = infraEnv

		infraEnvData, err := yaml.Marshal(infraEnv)
		if err != nil {
			return errors.Wrap(err, "failed to marshal agent installer infraEnv")
		}

		i.File = &asset.File{
			Filename: infraEnvFilename,
			Data:     infraEnvData,
		}
	}

	return i.finish()
}

// Files returns the files generated by the asset.
func (i *InfraEnv) Files() []*asset.File {
	if i.File != nil {
		return []*asset.File{i.File}
	}
	return []*asset.File{}
}

// Load returns infraenv asset from the disk.
func (i *InfraEnv) Load(f asset.FileFetcher) (bool, error) {

	file, err := f.FetchByName(infraEnvFilename)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrap(err, fmt.Sprintf("failed to load %s file", infraEnvFilename))
	}

	config := &aiv1beta1.InfraEnv{}
	if err := yaml.UnmarshalStrict(file.Data, config); err != nil {
		return false, errors.Wrapf(err, "failed to unmarshal %s", infraEnvFilename)
	}
	// If defined, convert to RpmArch amd64 -> x86_64 or arm64 -> aarch64
	if config.Spec.CpuArchitecture != "" {
		config.Spec.CpuArchitecture = arch.RpmArch(config.Spec.CpuArchitecture)
	}
	i.File, i.Config = file, config
	if err = i.finish(); err != nil {
		return false, err
	}

	return true, nil
}

func (i *InfraEnv) finish() error {

	if i.Config == nil {
		return errors.New("missing configuration or manifest file")
	}

	// Throw an error if CpuArchitecture isn't x86_64, aarch64, ppc64le, or ""
	switch i.Config.Spec.CpuArchitecture {
	case arch.RpmArch(types.ArchitectureAMD64), arch.RpmArch(types.ArchitectureARM64), arch.RpmArch(types.ArchitecturePPC64LE), "":
	default:
		return errors.Errorf("Config.Spec.CpuArchitecture %s is not supported ", i.Config.Spec.CpuArchitecture)
	}
	return nil
}
