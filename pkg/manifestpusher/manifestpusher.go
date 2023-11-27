package manifestpusher

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/util/wait"

	buildv1 "github.com/openshift/api/build/v1"
)

const (
	nodeArchitectureLabel = "kubernetes.io/arch"
)

type ManifestPusher interface {
	PushImageWithManifest(builds []buildv1.Build, targetImageRef string) error
}

func NewManifestPusher(logger *logrus.Entry, registryURL string, dockercfgPath string) ManifestPusher {
	return &manifestPusher{
		logger:        logger,
		registryURL:   registryURL,
		dockercfgPath: dockercfgPath,
	}
}

type manifestPusher struct {
	logger        *logrus.Entry
	registryURL   string
	dockercfgPath string
}

// PushImageWithManifest constructs a manifest-tool command to create and push a new image with all images that we built
// in the manifest list based on their architecture.
//
// Example command:
// /usr/bin/manifest-tool push from-args \
// --platforms linux/amd64,linux/arm64 \
// --template registry.multi-build01.arm-build.devcluster.openshift.com/ci/managed-clonerefs:latest-ARCH \
// --target registry.multi-build01.arm-build.devcluster.openshift.com/ci/managed-clonerefs:latest
func (m manifestPusher) PushImageWithManifest(builds []buildv1.Build, targetImageRef string) error {
	return wait.ExponentialBackoff(wait.Backoff{
		Steps:    5,
		Duration: 20 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
	}, func() (bool, error) {
		args := m.args(builds, targetImageRef)
		cmd := exec.Command("manifest-tool", args...)

		cmdOutput := &bytes.Buffer{}
		cmdError := &bytes.Buffer{}
		cmd.Stdout = cmdOutput
		cmd.Stderr = cmdError

		m.logger.Debugf("Running command: %s", cmd.String())
		err := cmd.Run()
		if err != nil {
			m.logger.WithError(err).WithField("output", cmdOutput.String()).WithField("error_output", cmdError.String()).Error("manifest-tool command failed")
			return false, nil
		}
		m.logger.WithField("output", cmdOutput.String()).Debug("manifest-tool command succeeded")

		m.logger.Infof("Image %s created", targetImageRef)
		return true, nil
	})
}

func (m manifestPusher) args(builds []buildv1.Build, targetImageRef string) []string {
	var template string
	platforms := make([]string, 0, len(builds))
	args := []string{
		"--debug",
		"--insecure",
		"--docker-cfg", m.dockercfgPath,
		"push", "from-args",
	}

	for i := range builds {
		build := &builds[i]
		arch := build.Spec.NodeSelector[nodeArchitectureLabel]
		platforms = append(platforms, fmt.Sprintf("linux/%s", arch))
		nameWithPlaceholder := strings.Replace(build.Spec.Output.To.Name, arch, "ARCH", 1)
		template = fmt.Sprintf("%s/%s/%s", m.registryURL, build.Spec.Output.To.Namespace, nameWithPlaceholder)
	}

	args = append(args, "--platforms", strings.Join(platforms, ","))
	args = append(args, "--template", template)
	return append(args, "--target", fmt.Sprintf("%s/%s", m.registryURL, targetImageRef))
}
