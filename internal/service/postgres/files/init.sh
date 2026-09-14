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
# Capture all of the O-Cloud Manager service database credentials
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

    # Create the user. Bind the password with psql's variable substitution
    # (:'pw') rather than interpolating it into the SQL string, so psql handles
    # the string-literal quoting/escaping regardless of the password contents.
    # -X (--no-psqlrc) skips startup files (e.g. ~/.psqlrc) so a stray
    # "\set pw ..." there cannot override -v pw and silently set the wrong
    # password (errors are ignored below with "|| true").
    psql -X -U postgres -v pw="${password}" -c "CREATE USER ${service_name} WITH PASSWORD :'pw';" || true

    # Create the database
    psql -X -U postgres -c "CREATE DATABASE ${service_name} OWNER ${service_name};" || true

    # Grant privileges (safe to run multiple times)
    psql -X -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE ${service_name} TO ${service_name};" || true

    echo "Completed setup for service: ${service_name}"
done
