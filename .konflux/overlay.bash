#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
SCRIPT_NAME=$(basename "$(readlink -f "${BASH_SOURCE[0]}")")

check-preconditions() {
    # yq must be available
    command -v yq >/dev/null 2>&1 || { echo "Error: yq is missing"; exit 1; }
}

overlay-images() {
    echo "Overlaying images (sha256)..."

    for image in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "replacing: name: $name, source: ${IMAGE_TO_SOURCE[$name]}, target: ${IMAGE_TO_TARGET[$name]}"
        sed -i "s,${IMAGE_TO_SOURCE[$name]},${IMAGE_TO_TARGET[$name]},g" $ARG_CSV_FILE
    done

    echo "Overlaying images completed"
    return 0
}

overlay-related-images() {
    echo "Overlaying related images..."

    declare -i index=0
    for image in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "adding related image: name: $name source: ${IMAGE_TO_SOURCE[$name]}, target: ${IMAGE_TO_TARGET[$name]}"
        yq e -i ".spec.relatedImages[$index].name=\"${IMAGE_TO_TARGET[$name]}\" |
                 .spec.relatedImages[$index].value=\"${IMAGE_TO_TARGET[$name]}\"" $ARG_CSV_FILE
        index=$index+1
    done

    echo "Overlaying related images completed"
    return 0
}

parse-images-file() {
    echo "Parsing image files..."

    if [[ ! -f "$ARG_IMAGES_FILE" ]]; then
        echo "Error: File '$ARG_IMAGES_FILE' not found!" >&2
        exit 1
    fi

    # Declare associative arrays
    declare -gA IMAGE_TO_SOURCE=()
    declare -gA IMAGE_TO_TARGET=()

    while IFS= read -r line; do
        # Skip empty lines and comments
        [[ -z "$line" ]] || [[ "$line" == \#* ]] && continue

        # Extract fields
        read -r name source_image target_image <<< "$line"

        # Store in associative arrays
        IMAGE_TO_SOURCE["$name"]="$source_image"
        IMAGE_TO_TARGET["$name"]="$target_image"
    done < "$ARG_IMAGES_FILE"

    echo "Parsing image completed..."
    return 0
}

parse-args() {
    echo "Parsing args..."

   # command line options
   local options=
   local long_options="set-images-file:,set-csv-file:,help"

   local parsed
   parsed=$(getopt --options="$options" --longoptions="$long_options" --name "$SCRIPT_NAME" -- "$@")
   eval set -- "$parsed"

   while true; do
      case $1 in
         --help)
            usage
            exit
            ;;
         --set-csv-file)
            declare -g ARG_CSV_FILE=$2
            shift 2
            ;;
         --set-images-file)
            declare -g ARG_IMAGES_FILE=$2
            shift 2
            ;;
         --)
            shift
            break
            ;;
         *)
            echo "Unexpected option: $1" >&2
            usage
            exit 1
            ;;
      esac
   done
   echo "Parsing args completed..."
}

main() {
   check-preconditions
   parse-args "$@"
   parse-images-file
   overlay-images
   overlay-related-images
    # Access the arrays
    echo "=== Image Mapping Summary ==="
    for name in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "Name: $name"
        echo "  Source: ${IMAGE_TO_SOURCE[$name]}"
        echo "  Target: ${IMAGE_TO_TARGET[$name]}"
        echo "------------------------------"
    done
}

usage() {
   cat << EOF
NAME

   $SCRIPT_NAME - overlay operator manifests for konflux

SYNOPSIS

   $SCRIPT_NAME --set-images-file FILE --set-csv-file FILE

DESCRIPTION

   overlay operator manifests

ARGS

   --set-images-file FILE
      Set the images file for the overlay

   --set-csv-file FILE
      Set the cluster service version file for the overlay

   --help
      Display this help and exit.

EOF
}

main "$@"
