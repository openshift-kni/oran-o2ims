#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Directories that are excluded from validation for various reasons:
#
# 1. config/manager, config/default, config/debug, config/manifests:
#    These require env-config.yaml which is generated during the build process
#    and is not committed to the repository.
#
# 2. docs/samples/git-setup/policytemplates/* :
#    These require the PolicyGenerator plugin, which is an external kustomize plugin
#    distributed via Red Hat container images (rhacm2/multicluster-operators-subscription-rhel9).
#    The plugin requires KUSTOMIZE_PLUGIN_HOME environment variable and
#    kustomize --enable-alpha-plugins flag.
#
# 3. docs/samples/git-setup/clustertemplates/* (parent and versioned):
#    These contain sample configurations with intentional resource ID conflicts
#    (e.g., duplicate ConfigMap names across different profile variants).
#    They are meant to be used as templates/examples, not built directly.
EXCLUDED_DIRS=(
    "./config/manager"
    "./config/default"
    "./config/debug"
    "./config/manifests"
    "./docs/samples/git-setup/clustertemplates"
    "./docs/samples/git-setup/clustertemplates/version_4.Y.Z"
    "./docs/samples/git-setup/clustertemplates/version_4.Y.Z+1"
    "./docs/samples/git-setup/policytemplates"
    "./docs/samples/git-setup/policytemplates/version_4.Y.Z"
    "./docs/samples/git-setup/policytemplates/version_4.Y.Z+1"
)

# Check if kustomize is installed
if ! command -v kustomize &> /dev/null; then
    echo -e "${RED}ERROR: kustomize is not installed${NC}"
    echo ""
    echo "Please install kustomize to run this check:"
    echo "  - macOS: brew install kustomize"
    echo "  - Linux: curl -s \"https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh\" | bash"
    echo "  - Manual: https://kubectl.docs.kubernetes.io/installation/kustomize/"
    echo ""
    exit 1
fi

echo "Checking all kustomization.yaml files can build successfully..."
echo ""

ERRORS=0
CHECKED=0
SKIPPED=0

# Helper function to check if directory should be excluded
is_excluded() {
    local dir="$1"
    for excluded in "${EXCLUDED_DIRS[@]}"; do
        if [ "$dir" = "$excluded" ]; then
            return 0
        fi
    done
    return 1
}

# Find all kustomization.yaml files
kustomize_files=()
while IFS= read -r file; do
    kustomize_files+=("$file")
done < <(find . -name 'kustomization.yaml' -not -path '*/venv/*' -not -path '*/.git/*' | sort)

if [ ${#kustomize_files[@]} -eq 0 ]; then
    echo -e "${YELLOW}WARNING: No kustomization.yaml files found${NC}"
    exit 0
fi

for kustomize_file in "${kustomize_files[@]}"; do
    dir=$(dirname "$kustomize_file")
    echo -n "  $dir: "

    # Check if this directory requires external plugins or has other issues
    if is_excluded "$dir"; then
        echo -e "${BLUE}SKIPPED${NC} (requires external plugins or generated files)"
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    # Try to build the kustomization
    if kustomize build "$dir" > /dev/null 2>&1; then
        echo -e "${GREEN}OK${NC}"
        CHECKED=$((CHECKED + 1))
    else
        echo -e "${RED}FAILED${NC}"
        echo -e "${YELLOW}    Error details:${NC}"
        kustomize build "$dir" 2>&1 | sed 's/^/    /'
        echo ""
        ERRORS=$((ERRORS + 1))
        CHECKED=$((CHECKED + 1))
    fi
done

echo ""
echo "Summary: Checked $CHECKED kustomization.yaml files, skipped $SKIPPED (require external plugins or generated files)"

if [[ $ERRORS -eq 0 ]]; then
    echo -e "${GREEN}All kustomization files validated successfully!${NC}"
    exit 0
else
    echo -e "${RED}$ERRORS kustomization file(s) failed validation${NC}"
    exit 1
fi

