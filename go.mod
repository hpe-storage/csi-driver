module github.com/hpe-storage/csi-driver

go 1.19

require (
	github.com/Scalingo/go-etcd-lock v3.0.1+incompatible
	github.com/container-storage-interface/spec v1.7.0
	github.com/golang/protobuf v1.5.4
	github.com/hpe-storage/common-host-libs v0.0.0-20241115050201-ebb9017f35d3
	github.com/hpe-storage/k8s-custom-resources v0.0.0-20240118202512-5f62990a7c2d
	github.com/kubernetes-csi/csi-lib-utils v0.11.0
	github.com/kubernetes-csi/csi-test v2.1.0+incompatible
	github.com/kubernetes-csi/external-snapshotter/client/v6 v6.1.0
	github.com/spf13/cobra v1.6.0
	github.com/stretchr/testify v1.8.1
	golang.org/x/mod v0.14.0
	golang.org/x/net v0.23.0
	google.golang.org/grpc v1.56.3
	k8s.io/api v0.29.0
	k8s.io/apimachinery v0.29.0
	k8s.io/client-go v0.29.0
	k8s.io/kubernetes v1.25.16
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/etcd v3.3.17+incompatible // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20230601102743-20bbbf26f4d8 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.0.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gopherjs/gopherjs v1.17.2 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jtolds/gls v4.20.0+incompatible // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/onsi/ginkgo v1.16.4 // indirect
	github.com/onsi/gomega v1.27.4 // indirect
	github.com/opencontainers/selinux v1.10.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/smarty/assertions v1.15.0 // indirect
	github.com/sparrc/go-ping v0.0.0-20190613174326-4e5b6552494c // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
	golang.org/x/crypto v0.21.0 // indirect
	golang.org/x/oauth2 v0.12.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/term v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20230525234020-1aefcd67740a // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230525234030-28d5490b6b19 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/errgo.v1 v1.0.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.25.16 // indirect
	k8s.io/cloud-provider v0.25.0 // indirect
	k8s.io/component-base v0.25.16 // indirect
	k8s.io/component-helpers v0.25.16 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/kube-openapi v0.0.0-20231010175941-2dd684a91f00 // indirect
	k8s.io/mount-utils v0.25.0 // indirect
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.25.16

replace k8s.io/api => k8s.io/api v0.25.16

replace k8s.io/apimachinery => k8s.io/apimachinery v0.26.0-alpha.0

replace k8s.io/client-go => k8s.io/client-go v0.25.16

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.25.16

replace k8s.io/apiserver => k8s.io/apiserver v0.25.16

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.25.16

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.25.16

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.25.16

replace k8s.io/code-generator => k8s.io/code-generator v0.25.16

replace k8s.io/component-base => k8s.io/component-base v0.25.16

replace k8s.io/component-helpers => k8s.io/component-helpers v0.25.16

replace k8s.io/controller-manager => k8s.io/controller-manager v0.25.16

replace k8s.io/cri-api => k8s.io/cri-api v0.25.16

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.25.16

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.25.16

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.25.16

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.25.16

replace k8s.io/kubectl => k8s.io/kubectl v0.25.16

replace k8s.io/kubelet => k8s.io/kubelet v0.25.16

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.25.16

replace k8s.io/metrics => k8s.io/metrics v0.25.16

replace k8s.io/mount-utils => k8s.io/mount-utils v0.25.16

replace k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.25.16

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.25.16

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.25.16

replace k8s.io/sample-controller => k8s.io/sample-controller v0.25.16

replace k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20230525220651-2546d827e515

replace google.golang.org/protobuf => google.golang.org/protobuf v1.33.0
