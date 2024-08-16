module github.com/openshift-kni/oran-o2ims

go 1.21

// Needed for importing the siteconfig operator, taken from the siteconfig operator repo.
replace (
	github.com/openshift/assisted-service/models => github.com/openshift/assisted-service/models v0.0.0-20230831114549-1922eda29cf8
	k8s.io/api => k8s.io/api v0.28.2
	k8s.io/client-go => k8s.io/client-go v0.28.2
)

require (
	github.com/PaesslerAG/jsonpath v0.1.1
	github.com/coreos/go-semver v0.3.1
	github.com/go-logr/logr v1.4.1
	github.com/go-task/slim-sprig/v3 v3.0.0
	github.com/golang-jwt/jwt/v4 v4.5.0
	github.com/google/uuid v1.5.0
	github.com/gorilla/mux v1.8.1
	github.com/imdario/mergo v0.3.15
	github.com/itchyny/gojq v0.12.14
	github.com/json-iterator/go v1.1.12
	github.com/onsi/ginkgo/v2 v2.14.0
	github.com/onsi/gomega v1.30.0
	github.com/openshift-kni/oran-o2ims/api/hardwaremanagement v0.0.0-00010101000000-000000000000
	github.com/peterhellberg/link v1.2.0
	github.com/prometheus/client_golang v1.18.0
	github.com/spf13/cobra v1.7.0
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace
	github.com/stolostron/siteconfig v0.0.0-20240712140409-8a222f6a98d4
	github.com/thoas/go-funk v0.9.3
	github.com/xeipuuv/gojsonschema v1.2.0
	go.uber.org/mock v0.4.0
	golang.org/x/net v0.25.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.29.1
	k8s.io/apimachinery v0.29.1
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog/v2 v2.120.1
	k8s.io/utils v0.0.0-20240102154912-e7106e64919e
	open-cluster-management.io/api v0.14.0
	open-cluster-management.io/governance-policy-propagator v0.12.0
	sigs.k8s.io/controller-runtime v0.17.2
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/PaesslerAG/gval v1.0.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.8.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/go-openapi/analysis v0.21.2 // indirect
	github.com/go-openapi/errors v0.20.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/loads v0.21.1 // indirect
	github.com/go-openapi/spec v0.20.7 // indirect
	github.com/go-openapi/strfmt v0.21.3 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/go-openapi/validate v0.22.0 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20230502171905-255e3b9b56de // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/itchyny/timefmt-go v0.1.5 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.4 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/metal3-io/baremetal-operator/apis v0.5.1 // indirect
	github.com/metal3-io/baremetal-operator/pkg/hardwareutils v0.4.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/openshift/api v0.0.0-20240124164020-e2ce40831f2e // indirect
	github.com/openshift/assisted-service/api v0.0.0-20240405132132-484ec5c683c6 // indirect
	github.com/openshift/assisted-service/models v0.0.0 // indirect
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87 // indirect
	github.com/openshift/hive/apis v0.0.0-20240306163002-9c5806a63531 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	go.mongodb.org/mongo-driver v1.10.0 // indirect
	golang.org/x/exp v0.0.0-20230522175609-2e198f4a06a1 // indirect
	golang.org/x/oauth2 v0.12.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/term v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.16.1 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gorm.io/gorm v1.24.5 // indirect
	k8s.io/apiextensions-apiserver v0.29.0 // indirect
	k8s.io/component-base v0.29.1 // indirect
	k8s.io/kube-openapi v0.0.0-20231010175941-2dd684a91f00 // indirect
	open-cluster-management.io/multicloud-operators-subscription v0.11.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace github.com/openshift-kni/oran-o2ims/api/hardwaremanagement => ./api/hardwaremanagement
