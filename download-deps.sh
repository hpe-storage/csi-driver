#!/bin/bash

VERSION=${1#"v"}
echo $VERSION
if [ -z "$VERSION" ]; then
  echo "Please specify the Kubernetes version: e.g."
  echo "./download-deps.sh v1.21.0"
  exit 1
fi

set -euo pipefail

# Find out all the replaced imports, make a list of them.
MODS=($(
  curl -sS "https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.24/go.mod" |
    sed -n 's|.*k8s.io/\(.*\) => ./staging/src/k8s.io/.*|k8s.io/\1|p'
))

# Now add those similar replace statements in the local go.mod file, but first find the version that
# the Kubernetes is using for them.
for MOD in "${MODS[@]}"; do
echo "Download $MOD"
  V=$(
    go mod download -json "${MOD}@kubernetes-${VERSION}" |
      sed -n 's|.*"Version": "\(.*\)".*|\1|p'
  )

  go mod edit "-replace=${MOD}=${MOD}@${V}"
done

go get "k8s.io/kubernetes@v${VERSION}"
go mod download
echo "done"#!/bin/bash

VERSION=${1#"v"}
echo $VERSION
if [ -z "$VERSION" ]; then
  echo "Please specify the Kubernetes version: e.g."
  echo "./download-deps.sh v1.21.0"
  exit 1
fi

set -euo pipefail

# Find out all the replaced imports, make a list of them.
MODS=($(
  curl -sS "https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.24/go.mod" |
    sed -n 's|.*k8s.io/\(.*\) => ./staging/src/k8s.io/.*|k8s.io/\1|p'
))

# Now add those similar replace statements in the local go.mod file, but first find the version that
# the Kubernetes is using for them.
for MOD in "${MODS[@]}"; do
echo "Download $MOD"
  V=$(
    go mod download -json "${MOD}@kubernetes-${VERSION}" |
      sed -n 's|.*"Version": "\(.*\)".*|\1|p'
  )

  go mod edit "-replace=${MOD}=${MOD}@${V}"
done

go get "k8s.io/kubernetes@v${VERSION}"
go mod download
echo "done"

