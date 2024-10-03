#!/bin/bash

# Create a temporary directory for the virtual environment
VENVDIR=$(mktemp --tmpdir -d venv.XXXXXX) || {
    echo "Failed to create working directory" >&2
    exit 1
}

function cleanup {
    # Clean up the temporary directory
    rm -rf "${VENVDIR}"
}
trap cleanup EXIT

function fatal {
    # Log an error message and exit with a non-zero status
    echo "ERROR: $*" >&2
    exit 1
}

# Create a Python virtual environment
python3 -m venv "${VENVDIR}" || fatal "Could not set up virtualenv"

# Activate the virtual environment
# shellcheck disable=SC1091
source "${VENVDIR}/bin/activate" || fatal "Could not activate virtualenv"

# Install bashate in the virtual environment
pip install bashate==2.1.0 || fatal "Installation of bashate failed"

# Run bashate on all Bash script files, excluding specified directories
find . -name '*.sh' -not -path './vendor/*' -not -path './*/vendor/*' -not -path './git/*' \
    -not -path './bin/*' -not -path './testbin/*' -print0 \
    | xargs -0 --no-run-if-empty bashate -v -e 'E*' -i E006

# Check the exit status of bashate and exit with an appropriate status
if [ $? -eq 0 ]; then
    echo "All checks passed successfully"
    exit 0
else
    echo "Some checks failed. See error messages above."
    exit 2
fi
