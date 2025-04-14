#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
SCRIPT_NAME=$(basename "$(readlink -f "${BASH_SOURCE[0]}")")

MAP_STAGING="staging"
MAP_PRODUCTION="production"

check_preconditions() {
    echo "Checking pre-conditions..."

    # yq must be installed
    command -v yq >/dev/null 2>&1 || { echo "Error: yq seems not to be installed.Exit!"; exit 1; }
    echo "Checking pre-conditions completed!"
    return 0
}

overlay_images() {
    echo "Overlaying images (sha256)..."

    for image_name in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "replacing: image_name: $image_name, source: ${IMAGE_TO_SOURCE[$image_name]}, target: ${IMAGE_TO_TARGET[$image_name]}"
        sed -i "s,${IMAGE_TO_SOURCE[$image_name]},${IMAGE_TO_TARGET[$image_name]},g" $ARG_CSV_FILE
    done

    echo "Overlaying images completed!"
    return 0
}

overlay_related_images() {
    echo "Overlaying related images..."

    # remove the existing section
    yq e -i 'del(.spec.relatedImages)' $ARG_CSV_FILE

    # create a new section from scratch
    declare -i index=0
    for image_name in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "adding related image: image_name: $image_name source: ${IMAGE_TO_SOURCE[$image_name]}, target: ${IMAGE_TO_TARGET[$image_name]}"
        yq e -i ".spec.relatedImages[$index].name=\"$image_name\" |
                 .spec.relatedImages[$index].value=\"${IMAGE_TO_TARGET[$image_name]}\"" $ARG_CSV_FILE
        index=$index+1
    done

    echo "Overlaying related images completed!"
    return 0
}

parse_map_images_file() {
    echo "Parsing map image file..."

    if [[ ! -f "$ARG_MAP_FILE" ]]; then
        echo "Error: File '$ARG_MAP_FILE' not found. Exit!" >&2
        exit 1
    fi

    # Extract keys and images
    local keys=($(yq eval '.[].key' "$ARG_MAP_FILE"))
    local staging_images=($(yq eval '.[].stage' "$ARG_MAP_FILE"))
    local production_images=($(yq eval '.[].production' "$ARG_MAP_FILE"))
    local entries=${#keys[@]}

    # Declare associative arrays
    declare -gA IMAGE_TO_STAGING=()
    declare -gA IMAGE_TO_PRODUCTION=()

    declare -i i=0
    for ((; i<entries; i++)); do
        # Store in associative arrays
        local key=${keys[i]}
        IMAGE_TO_STAGING["$key"]="${staging_images[i]}"
        IMAGE_TO_PRODUCTION["$key"]="${production_images[i]}"
    done

    echo "Parsing map image files completed!"
    return 0
}

map_images() {
    echo "Mapping images ..."

    parse_map_images_file

    for image_name in "${!IMAGE_TO_TARGET[@]}"; do
        local image_name_target="${IMAGE_TO_TARGET[$image_name]}"
        local image_name_target_trimmed="${image_name_target%@*}"

        local image_name_target_trimmed_mapped=""
        if [[ "$ARG_MAP" == "$MAP_STAGING" ]]; then
            image_name_target_trimmed_mapped="${IMAGE_TO_STAGING[$image_name]}"
        elif [[ "$ARG_MAP" == "$MAP_PRODUCTION" ]]; then
            image_name_target_trimmed_mapped="${IMAGE_TO_PRODUCTION[$image_name]}"
        fi

        echo "replacing: image_name: $image_name, original: $image_name_target_trimmed, mapped: $image_name_target_trimmed_mapped"
        sed -i "s,$image_name_target_trimmed,$image_name_target_trimmed_mapped,g" $ARG_CSV_FILE
    done

    echo "Mapping images completed"
}

parse_overlay_images_file() {
    echo "Parsing image files..."

    if [[ ! -f "$ARG_IMAGES_FILE" ]]; then
        echo "Error: File '$ARG_IMAGES_FILE' not found. Exit!" >&2
        exit 1
    fi

    # Extract keys and images
    local keys=($(yq eval '.[].key' "$ARG_IMAGES_FILE"))
    local sources=($(yq eval '.[].source' "$ARG_IMAGES_FILE"))
    local targets=($(yq eval '.[].target' "$ARG_IMAGES_FILE"))
    local entries=${#keys[@]}

    # Declare associative arrays
    declare -gA IMAGE_TO_SOURCE=()
    declare -gA IMAGE_TO_TARGET=()

    declare -i i=0
    for ((; i<entries; i++)); do
        # Store in associative arrays
        local key=${keys[i]}
        IMAGE_TO_SOURCE["$key"]="${sources[i]}"
        IMAGE_TO_TARGET["$key"]="${targets[i]}"
    done

    echo "Parsing image completed!"
    return 0
}

parse_args() {
    echo "Parsing args..."

   # command line options
   local options=
   local long_options="set-images-file:,set-map-file:,set-csv-file:,set-map-staging,set-map-production,help"

   local parsed=$(getopt --options="$options" --longoptions="$long_options" --name "$SCRIPT_NAME" -- "$@")
   eval set -- "$parsed"

   local map_staging=0
   local map_production=0
   declare -g ARG_MAP_FILE=""
   declare -g ARG_IMAGES_FILE=""
   declare -g ARG_CSV_FILE=""
   declare -g ARG_MAP=""
   while true; do
      case $1 in
         --help)
            usage
            exit
            ;;
         --set-csv-file)
            ARG_CSV_FILE=$2
            shift 2
            ;;
         --set-images-file)
            ARG_IMAGES_FILE=$2
            shift 2
            ;;
         --set-map-file)
            ARG_MAP_FILE=$2
            shift 2
            ;;
         --set-map-staging)
            map_staging=1
            ARG_MAP=$MAP_STAGING
            shift 1
            ;;
         --set-map-production)
            map_production=1
            ARG_MAP=$MAP_PRODUCTION
            shift 1
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

   # validate images file
   if [[ -n $ARG_IMAGES_FILE && ! -f "$ARG_IMAGES_FILE" ]]; then
       echo "Error: file '$ARG_IMAGES_FILE' does not exist.Exit!"
       exit 1
   fi

   # validate csv file
   if [[ -n $ARG_CSV_FILE && ! -f "$ARG_CSV_FILE" ]]; then
       echo "Error: file '$ARG_CSV_FILE' does not exist.Exit!"
       exit 1
   fi

   # validate map options
   if [[ $map_staging -eq 1 && $map_production -eq 1 ]]; then
       echo "Error: cannot specify both '--set-map-staging' and '--set-map-production'.Exit!"
       exit 1
   fi

   if [[ $map_staging -eq 1 || $map_production -eq 1 ]]; then
       if [[ ! -n $ARG_MAP_FILE ]]; then
           echo "Error: specify '--set-map-file' to use a container registry map file.Exit!!"
           exit 1
       fi
   fi

   if [[ $map_staging -eq 0 && $map_production -eq 0 ]]; then
       if [[ -n $ARG_MAP_FILE ]]; then
           echo "Error: specify '--set-map-staging' or '--set-map-production'.Exit!"
           exit 1
       fi
   fi

   if [[ -n $ARG_MAP_FILE && ! -f "$ARG_MAP_FILE" ]]; then
       echo "Error: file '$ARG_MAP_FILE' does not exist.Exit!"
       exit 1
   fi

   echo "Parsing args completed..."
}

main() {
   check_preconditions
   parse_args "$@"
   parse_overlay_images_file
   overlay_images
   overlay_related_images
   # this must always be the last action
   map_images

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
