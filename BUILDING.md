
# Building the HPE CSI Driver

- Clone the repo : git clone <https://github.com/hpe-storage/csi-driver>
- cd to csi-driver
- Turn on go modules support `export GO111MODULES=on`
- Set GOOS `export GOOS=linux`(optional)
- Set CONTAINER_REGISTRY env to point to your image registry, if other than docker.io(default)
- Set GOPATH, as go binaries are placed under $(GOPATH)/bin which is added to $(PATH)
- Install [golangci-lint](https://github.com/golangci/golangci-lint#install)
- Run `make all`

Note1: Minimum go version of 1.12 required.
Note2: tests are only supported on Linux platform
