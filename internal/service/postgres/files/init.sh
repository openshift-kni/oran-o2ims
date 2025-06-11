#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -e

# Define an associative array of service names based on matching variables fitting this pattern:
#   ORAN_O2IMS_SERVICE_PASSWORD
declare -A services
# Capture all of the ORAN O2IMS service database credentials
for var in "${!ORAN_O2IMS@}"; do
    if [[ $var =~ ^ORAN_O2IMS_(.*)_PASSWORD ]]; then
        service_name="${BASH_REMATCH[1]}"
        password="${!var}"
        services[${service_name,,}]=$password
    fi
done

# Everything here is idempotent
for service_name in "${!services[@]}"; do
    password=${services[${service_name}]}

    echo "Processing database setup for service: ${service_name}"

    # Create the user
    psql -U postgres -c "CREATE USER ${service_name} WITH PASSWORD '${password}';" || true

    # Create the database
    psql -U postgres -c "CREATE DATABASE ${service_name} OWNER ${service_name};" || true

    # Grant privileges (safe to run multiple times)
    psql -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE ${service_name} TO ${service_name};"

    echo "Completed setup for service: ${service_name}"
done
