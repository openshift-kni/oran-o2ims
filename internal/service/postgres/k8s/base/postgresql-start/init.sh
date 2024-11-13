#!/bin/bash

set -e

# Define an array of service names
services=("alarms")

# Everything here is idempotent
for service_name in "${services[@]}"; do
    echo "Processing database setup for service: ${service_name}"

    # TODO: for prod generate password during deployment for each service
    # Create the user
    psql -U postgres -c "CREATE USER ${service_name} WITH PASSWORD '${service_name}';" || true

    # Create the database
    psql -U postgres -c "CREATE DATABASE ${service_name} OWNER ${service_name};" || true

    # Grant privileges (safe to run multiple times)
    psql -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE ${service_name} TO ${service_name};"

    echo "Completed setup for service: ${service_name}"
done
