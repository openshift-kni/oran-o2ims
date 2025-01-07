module github.com/openshift-kni/oran-o2ims

go 1.22.0

// Needed for importing the siteconfig operator, taken from the siteconfig operator repo.
replace github.com/openshift/assisted-service/models => github.com/openshift/assisted-service/models v0.0.0-20230831114549-1922eda29cf8

require (
	github.com/PaesslerAG/jsonpath v0.1.1
	github.com/coreos/go-semver v0.3.1
	github.com/getkin/kin-openapi v0.128.0
	github.com/go-logr/logr v1.4.2
	github.com/go-task/slim-sprig/v3 v3.0.0
	github.com/golang-jwt/jwt/v4 v4.5.1
	github.com/golang-migrate/migrate/v4 v4.18.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/imdario/mergo v1.0.0
	github.com/itchyny/gojq v0.12.17
	github.com/jackc/pgx/v5 v5.7.2
	github.com/json-iterator/go v1.1.12
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/oapi-codegen/nethttp-middleware v1.0.2
	github.com/oapi-codegen/oapi-codegen/v2 v2.4.1
	github.com/oapi-codegen/runtime v1.1.1
	github.com/onsi/ginkgo/v2 v2.22.2
	github.com/onsi/gomega v1.36.2
	github.com/openshift-kni/cluster-group-upgrades-operator v0.0.0-20241213003211-a57a58a5c4f2
	github.com/openshift-kni/lifecycle-agent v0.0.0-20241010194013-9d0e25438512
	github.com/openshift-kni/oran-o2ims/api/hardwaremanagement v0.0.0-20241001130125-a052f08603f7
	github.com/openshift-kni/oran-o2ims/api/inventory v0.0.0-00010101000000-000000000000
	github.com/openshift-kni/oran-o2ims/api/provisioning v0.0.0-00010101000000-000000000000
	github.com/openshift/api v0.0.0-20240423014330-2cb60a113ad1
	github.com/openshift/assisted-service/api v0.0.0-20240405132132-484ec5c683c6
	github.com/peterhellberg/link v1.2.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.76.2
	github.com/prometheus/client_golang v1.20.5
	github.com/r3labs/diff/v3 v3.0.1
	github.com/spf13/cobra v1.8.1
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace
	github.com/stephenafamo/bob v0.28.0
	github.com/stolostron/siteconfig v0.0.0-20241003162917-06ef126f7eba
	github.com/thoas/go-funk v0.9.3
	github.com/xeipuuv/gojsonschema v1.2.0
	go.uber.org/mock v0.5.0
	golang.org/x/net v0.33.0
	golang.org/x/oauth2 v0.24.0
	golang.org/x/sync v0.10.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.31.4
	k8s.io/apimachinery v0.31.4
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog/v2 v2.130.1
	k8s.io/utils v0.0.0-20240902221715-702e33fdd3c3
	open-cluster-management.io/api v0.15.0
	open-cluster-management.io/governance-policy-propagator v0.15.0
	sigs.k8s.io/controller-runtime v0.19.3
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/PaesslerAG/gval v1.0.0 // indirect
	github.com/aarondl/json v0.0.0-20221020222930-8b0db17ef1bf // indirect
	github.com/aarondl/opt v0.0.0-20230114172057-b91f370c41f0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dprotaso/go-yit v0.0.0-20220510233725-9ba8df137936 // indirect
	github.com/emicklei/go-restful/v3 v3.12.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-openapi/analysis v0.21.2 // indirect
	github.com/go-openapi/errors v0.22.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/loads v0.21.1 // indirect
	github.com/go-openapi/spec v0.20.7 // indirect
	github.com/go-openapi/strfmt v0.23.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-openapi/validate v0.22.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.9-0.20230804172637-c7be7c783f49 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20241210010833-40e02aabc2ad // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/yaml v0.3.1 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/jackc/pgerrcode v0.0.0-20220416144525-469b46aa5efa // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.4 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/metal3-io/baremetal-operator/apis v0.5.1 // indirect
	github.com/metal3-io/baremetal-operator/pkg/hardwareutils v0.4.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/openshift/assisted-service/models v0.0.0 // indirect
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87 // indirect
	github.com/openshift/hive/apis v0.0.0-20240306163002-9c5806a63531 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/qdm12/reprint v0.0.0-20200326205758-722754a53494 // indirect
	github.com/sergi/go-diff v1.3.1 // indirect
	github.com/speakeasy-api/openapi-overlay v0.9.0 // indirect
	github.com/stephenafamo/scan v0.6.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.3.5 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/vmware-labs/yaml-jsonpath v0.3.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	go.mongodb.org/mongo-driver v1.17.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/mod v0.22.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/term v0.27.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.28.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/protobuf v1.36.1 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gorm.io/gorm v1.24.5 // indirect
	k8s.io/apiextensions-apiserver v0.31.2 // indirect
	k8s.io/kube-openapi v0.0.0-20240521193020-835d969ad83a // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace github.com/openshift-kni/oran-o2ims/api/hardwaremanagement => ./api/hardwaremanagement

replace github.com/openshift-kni/oran-o2ims/api/inventory => ./api/inventory

replace github.com/openshift-kni/oran-o2ims/api/provisioning => ./api/provisioning

// From the mergo project's README: "If the vanity URL is causing issues in
// your project due to a dependency pulling Mergo - it isn't a direct
// dependency in your project - it is recommended to use replace to pin the
// version to the last one with the old import URL:"
replace github.com/imdario/mergo => github.com/imdario/mergo v0.3.16

replace k8s.io/client-go => k8s.io/client-go v0.31.4
