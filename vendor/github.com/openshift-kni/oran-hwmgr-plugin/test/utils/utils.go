/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//nolint:all
package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/crds"

	"github.com/integralist/go-findroot/find"
	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	"golang.org/x/mod/modfile"
)

const (
	prometheusOperatorVersion = "v0.68.0"
	prometheusOperatorURL     = "https://github.com/prometheus-operator/prometheus-operator/" +
		"releases/download/%s/bundle.yaml"

	certmanagerVersion = "v1.5.3"
	certmanagerURLTmpl = "https://github.com/jetstack/cert-manager/releases/download/%s/cert-manager.yaml"
)

func warnError(err error) {
	fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// InstallPrometheusOperator installs the prometheus Operator to be used to export the enabled metrics.
func InstallPrometheusOperator() error {
	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	cmd := exec.Command("kubectl", "create", "-f", url)
	_, err := Run(cmd)
	return err
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) ([]byte, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}

// UninstallPrometheusOperator uninstalls the prometheus
func UninstallPrometheusOperator() {
	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := Run(cmd)
	return err
}

// LoadImageToKindCluster loads a local docker image to the kind cluster
func LoadImageToKindClusterWithName(name string) error {
	cluster := "kind"
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	cmd := exec.Command("kind", kindOptions...)
	_, err := Run(cmd)
	return err
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

// GetGitCommitFromPseudoVersion gets the git commit from the pseudo version
// the pseudo version parameter is like 'v0.0.0-20241119215836-4bf01fa3f48'
func GetGitCommitFromPseudoVersion(pseudoVersion string) string {

	tokens := strings.Split(pseudoVersion, "-")
	commit := tokens[len(tokens)-1]
	return commit
}

// GetHardwareManagementGitRepoFromModule gets the git repository from the modPath of a HardwareManagement module
// the modPath parameter is like 'github.com/rauherna/oran-o2ims/api/hardwaremanagement'
func GetHardwareManagementGitRepoFromModule(modPath string) string {

	hwrMgtRepo := strings.TrimRight(modPath, "/"+crds.ImsHwrMgtPath)
	return hwrMgtRepo
}

// GetModuleFromGoMod gets the module path (considering 'replace') and pseudo version processing the project go.mod
// the modPath parameter is like 'github.com/openshift-kni/oran-o2ims/api/hardwaremanagement'
// the returned new module path is like 'github.com/rauherna/oran-o2ims/api/hardwaremanagement'
// the returned module pseudo version is like 'v0.0.0-20241119215836-4bf01fa3f48'
func GetModuleFromGoMod(modPath string) (modNewPath string, modPseudoVersion string, e error) {

	// find the project dir (the go.mod absolute path)
	prjDir, err := find.Repo()
	if err != nil {
		return "", "", fmt.Errorf("failed to find project root, err:%w", err)
	}

	// read go.mod
	goModFile := prjDir.Path + "/go.mod"
	goModData, err := os.ReadFile(goModFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read go.mod:%s, err:%w",
			goModFile, err)
	}

	// parse go.mod
	mods, err := modfile.Parse("go.mod", goModData, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse go.mod:%s, err:%w",
			goModFile, err)
	}

	// check if the module is 'replaced' in go.mod
	for _, modr := range mods.Replace {
		pathOld := modr.Old.Path

		if pathOld == modPath {

			pathNew := modr.New.Path
			versionNew := modr.New.Version

			return pathNew, versionNew, nil
		}
	}

	// check if the module is 'required' in go.mod (fallback)
	for _, modr := range mods.Require {
		path := modr.Mod.Path

		if path == modPath {

			version := modr.Mod.Version
			return path, version, nil
		}
	}

	return "", "",
		fmt.Errorf("failed to find module:%s in go.mod:%s", modPath, goModFile)
}
