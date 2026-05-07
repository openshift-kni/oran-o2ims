/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package teste2eutils

import (
	"fmt"
	"os"
	"strings"

	"github.com/integralist/go-findroot/find"
	"golang.org/x/mod/modfile"
	k8syaml "sigs.k8s.io/yaml"
)

// GetGitCommitFromPseudoVersion gets the git commit from the pseudo version
// the pseudo version parameter is like 'v0.0.0-20241119215836-4bf01fa3f48'
func GetGitCommitFromPseudoVersion(pseudoVersion string) string {
	tokens := strings.Split(pseudoVersion, "-")
	commit := tokens[len(tokens)-1]
	return commit
}

// GetModuleFromGoMod gets the module path (considering 'replace') and pseudo version processing the project go.mod
// the modPath parameter is like 'github.com/openshift-kni/oran-o2ims/api/hardwaremanagement'
// the returned new module path is like 'github.com/rauherna/oran-o2ims/api/hardwaremanagement'
// the returned module pseudo version is like 'v0.0.0-20241119215836-4bf01fa3f48'
func GetModuleFromGoMod(modPath string) (modNewPath, modPseudoVersion string, e error) {
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

// LoadYAML reads a YAML file and unmarshals it into a typed K8s resource.
func LoadYAML[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file %s: %w", path, err)
	}
	obj := new(T)
	if err := k8syaml.Unmarshal(data, obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s: %w", path, err)
	}
	return obj, nil
}
