module github.com/hpe-storage/csi-driver

go 1.13

require (
	github.com/Azure/go-autorest v11.1.2+incompatible // indirect
	github.com/Scalingo/go-etcd-lock v3.0.1+incompatible
	github.com/container-storage-interface/spec v1.3.0
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/etcd v3.3.17+incompatible // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/elazarl/goproxy v0.0.0-20180725130230-947c36da3153 // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/protobuf v1.4.2
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.12.0 // indirect
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/hpe-storage/common-host-libs v4.2.1-0.20210323052757-fffd89988096+incompatible
	github.com/hpe-storage/k8s-custom-resources v0.0.0-20201216052928-e2854a36f3f9
	github.com/jonboulle/clockwork v0.1.0 // indirect
	github.com/kubernetes-csi/csi-lib-utils v0.7.1
	github.com/kubernetes-csi/csi-test v2.1.0+incompatible
	github.com/kubernetes-csi/external-snapshotter/v2 v2.1.2
	github.com/onsi/ginkgo v1.11.0 // indirect
	github.com/onsi/gomega v1.7.1 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/smartystreets/goconvey v1.6.4 // indirect
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/sparrc/go-ping v0.0.0-20190613174326-4e5b6552494c // indirect
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.etcd.io/bbolt v1.3.3 // indirect
	go.uber.org/multierr v1.4.0 // indirect
	go.uber.org/zap v1.12.0 // indirect
	golang.org/x/mod v0.2.0
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
	golang.org/x/sys v0.0.0-20200615200032-f1bc736245b1
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/grpc v1.27.0
	gopkg.in/errgo.v1 v1.0.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	k8s.io/api v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/client-go v0.19.3
	k8s.io/cloud-provider v0.19.3 // indirect
	k8s.io/kubernetes v1.14.0
	k8s.io/utils v0.0.0-20201015054608-420da100c033
	sigs.k8s.io/structured-merge-diff/v3 v3.0.0 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace k8s.io/api => k8s.io/api v0.17.0

replace k8s.io/apimachinery => k8s.io/apimachinery v0.17.1-beta.0

replace k8s.io/client-go => k8s.io/client-go v0.17.0

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0
