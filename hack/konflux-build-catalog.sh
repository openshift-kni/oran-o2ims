## This is an script to start testing the konflux 'run-script' task ##

set -x

echo "update catalog template..."
./hack/konflux-update-catalog-template.sh --set-catalog-template-file .konflux/catalog/catalog-template.in.yaml --set-bundle-builds-file .konflux/catalog/bundle.builds.in.yaml

echo "render catalog template..."
OPM=./bin/opm
mkdir -p `dirname $OPM`
OS=linux
ARCH=amd64
curl -sSLo $OPM https://github.com/operator-framework/operator-registry/releases/download/v1.54.0/$OS-$ARCH-opm
chmod +x $OPM

# this should fail if: no internet access to 'registry.redhat.io' and/or no 'registry.redhat.io' credentials passed to opm
$OPM alpha render-template basic --output yaml --migrate-level bundle-object-to-csv-metadata .konflux/catalog/catalog-template.in.yaml > .konflux/catalog/o-cloud-manager/catalog.yaml

echo "overlay catalog images for production..."
sed -i 's|quay.io/redhat-user-workloads/telco-5g-tenant/o-cloud-manager-bundle-4-19|registry.redhat.io/openshift4/o-cloud-manager-operator-bundle|g' .konflux/catalog/o-cloud-manager/catalog.yaml

echo "validate catalog images for production..."
./hack/konflux-validate-related-images-production.sh --set-catalog-file .konflux/catalog/o-cloud-manager/catalog.yaml
