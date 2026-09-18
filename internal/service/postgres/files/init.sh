#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -euo pipefail

# Define an associative array of service names based on matching variables fitting this pattern:
#   ORAN_O2IMS_SERVICE_PASSWORD
declare -A services
# Capture all of the O-Cloud Manager service database credentials
for var in "${!ORAN_O2IMS@}"; do
    if [[ $var =~ ^ORAN_O2IMS_(.*)_PASSWORD$ ]]; then
        service_name="${BASH_REMATCH[1],,}"
        if [[ ! $service_name =~ ^[a-z_][a-z0-9_]*$ ]]; then
            echo "Invalid service database name: ${service_name}" >&2
            exit 1
        fi
        services["${service_name}"]="${!var}"
    fi
done

# Everything here is idempotent
for service_name in "${!services[@]}"; do
    password="${services[${service_name}]}"

    if [[ -z "${password}" ]]; then
        echo "Password is empty for service: ${service_name}" >&2
        exit 1
    fi

    echo "Processing database setup for service: ${service_name}"

    # Pass the password through the environment rather than the command line.
    # psql expands :'pw' only when it reads SQL from a script; it leaves the
    # placeholder unchanged when the SQL is passed with -c.
    (
        export O2IMS_DB_PASSWORD="${password}"
        trap 'unset O2IMS_DB_PASSWORD' EXIT
        psql -X -U postgres -v ON_ERROR_STOP=1 -v service_name="${service_name}" <<'SQL'
\getenv pw O2IMS_DB_PASSWORD

SELECT EXISTS (
    SELECT FROM pg_roles WHERE rolname = :'service_name'
) AS role_exists \gset

\if :role_exists
ALTER ROLE :"service_name" WITH LOGIN PASSWORD :'pw';
\else
CREATE USER :"service_name" WITH PASSWORD :'pw';
\endif

SELECT EXISTS (
    SELECT FROM pg_database WHERE datname = :'service_name'
) AS database_exists \gset

\if :database_exists
\else
CREATE DATABASE :"service_name" OWNER :"service_name";
\endif

GRANT ALL PRIVILEGES ON DATABASE :"service_name" TO :"service_name";
SQL
    )

    echo "Completed setup for service: ${service_name}"
done
