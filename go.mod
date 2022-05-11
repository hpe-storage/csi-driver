module github.com/hpe-storage/csi-driver

go 1.13

require (
	github.com/Scalingo/go-etcd-lock v3.0.1+incompatible
	github.com/container-storage-interface/spec v1.3.0
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/etcd v3.3.17+incompatible // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/golang/protobuf v1.5.2
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/hpe-storage/common-host-libs v4.6.1-0.20220509043010-9f9dfdcc3129+incompatible
	github.com/hpe-storage/k8s-custom-resources v0.0.0-20201216052928-e2854a36f3f9
	github.com/kubernetes-csi/csi-lib-utils v0.7.1
	github.com/kubernetes-csi/csi-test v2.1.0+incompatible
	github.com/kubernetes-csi/external-snapshotter/v2 v2.1.3
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sparrc/go-ping v0.0.0-20190613174326-4e5b6552494c // indirect
	github.com/spf13/cobra v1.4.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/mod v0.6.0-dev.0.20220106191415-9b9b3d81d5e3
	golang.org/x/net v0.0.0-20220127200216-cd36cc0744dd
	google.golang.org/grpc v1.40.0
	gopkg.in/errgo.v1 v1.0.1 // indirect
	k8s.io/api v0.24.0
	k8s.io/apiextensions-apiserver v0.24.0 // indirect
	k8s.io/apimachinery v0.24.0
	k8s.io/client-go v0.24.0
	k8s.io/cloud-provider v0.19.3 // indirect
	k8s.io/kubernetes v1.14.0
)

replace k8s.io/api => k8s.io/api v0.17.0

replace k8s.io/apimachinery => k8s.io/apimachinery v0.17.1-beta.0

replace k8s.io/client-go => k8s.io/client-go v0.17.0

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0
