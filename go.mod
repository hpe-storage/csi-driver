module github.com/hpe-storage/csi-driver

go 1.12

require (
	github.com/Scalingo/go-etcd-lock v3.0.1+incompatible
	github.com/container-storage-interface/spec v1.2.0
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/golang/groupcache v0.0.0-20191027212112-611e8accdfc9 // indirect
	github.com/golang/protobuf v1.3.2
	github.com/google/btree v1.0.0 // indirect
	github.com/google/go-cmp v0.3.1 // indirect
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.12.0 // indirect
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/hpe-storage/common-host-libs v0.0.0-20200601164253-a5ef69310997
	github.com/hpe-storage/k8s-custom-resources v0.0.0-20190828052325-42886f48215c
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kubernetes-csi/csi-lib-utils v0.6.1
	github.com/kubernetes-csi/csi-test v2.1.0+incompatible
	github.com/onsi/gomega v1.7.1 // indirect
	github.com/prometheus/client_golang v1.2.1 // indirect
	github.com/smartystreets/goconvey v1.6.4 // indirect
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/sparrc/go-ping v0.0.0-20190613174326-4e5b6552494c // indirect
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.uber.org/multierr v1.4.0 // indirect
	go.uber.org/zap v1.12.0 // indirect
	golang.org/x/mod v0.2.0
	golang.org/x/net v0.0.0-20191105084925-a882066a44e0
	golang.org/x/sys v0.0.0-20191105231009-c1f44814a5cd // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/genproto v0.0.0-20191028173616-919d9bdd9fe6 // indirect
	google.golang.org/grpc v1.24.0
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/errgo.v1 v1.0.1 // indirect
	k8s.io/api v0.16.12
	k8s.io/apimachinery v0.16.12
	k8s.io/client-go v0.16.12
	k8s.io/kubernetes v1.16.12
	k8s.io/utils v0.0.0-20200619165400-6e3d28b6ed19
)

replace (
	k8s.io/api => k8s.io/api v0.16.12
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.12
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.12
	k8s.io/apiserver => k8s.io/apiserver v0.16.12
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.16.12
	k8s.io/client-go => k8s.io/client-go v0.16.12
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.16.12
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.16.12
	k8s.io/code-generator => k8s.io/code-generator v0.16.12
	k8s.io/component-base => k8s.io/component-base v0.16.12
	k8s.io/cri-api => k8s.io/cri-api v0.16.12
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.16.12
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.16.12
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.16.12
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.16.12
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.16.12
	k8s.io/kubectl => k8s.io/kubectl v0.16.12
	k8s.io/kubelet => k8s.io/kubelet v0.16.12
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.16.12
	k8s.io/metrics => k8s.io/metrics v0.16.12
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.16.12
)
