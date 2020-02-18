# HPE CSI Driver for Kubernetes
A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for Kubernetes. The HPE CSI Driver for Kubernetes allows you to use a [Container Storage Provider](https://github.com/hpe-storage/container-storage-provider) to perform data management operations on storage resources.

## Deploying to Kubernetes
This guide is primarily written to accommodate installation on upstream Kubernetes. Installation steps may vary for different distributions of Kubernetes. Please see the [hpe-storage/co-deployments](https://github.com/hpe-storage/co-deployments) for additional methods, tweaks and platforms.

### Kubernetes Compatibility

<table>
  <thead>
    <tr>
      <th></th>
      <th colspan=3>Kubernetes Version</th>
      <th rowspan=2> Release Notes</th>
    </tr>
    <tr>
      <th>HPE CSI Driver</th>
      <th><= 1.12</th>
      <th>1.13+</th>
      <th>1.14+</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>v1.0.0</td>
      <td>no</td>
      <td>yes</td>
      <td>yes</td>
      <td><a href="release-notes/v1.0.0.md">v1.0.0</a></td>
    </tr>
  </tbody>
</table>

**Note:** Support Matrix for each release can be found at [SUPPORT.md](SUPPORT.md)

### Requirements
Different features mature at different rates. Refer to the [official table](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) of feature gates in the Kubernetes docs.

The following guidelines applies to what feature gates got introduced as alphas for the corresponding version of K8s. For example, `ExpandCSIVolumes` got introduced in 1.14 but is still an alpha in 1.15, hence you need to enable that feature gate in 1.15 as well if you want to use it.

Kubernetes 1.13
 * `--allow-privileged` flag must be set to true for the API server

Kubernetes 1.14
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=ExpandCSIVolumes=true,ExpandInUsePersistentVolumes=true` feature gate flags must be set to true for both the API server and kubelet for resize support

Kubernetes 1.15
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=ExpandCSIVolumes=true,ExpandInUsePersistentVolumes=true` feature gate flags must be set to true for both the API server and kubelet for resize support
 * `--feature-gates=CSIInlineVolume=true` feature gate flag must be set to true for both the API server and kubelet for pod inline volumes (Ephemeral Local Volumes) support
 * `--feature-gates=VolumePVCDataSource=true` feature gate flag must be set to true for both the API server and kubelet for Volume cloning support

### Deploying the HPE CSI Driver for Kubernetes
The following example walks through deployment of the driver with a HPE Nimble Storage CSP backend. Replace any Nimble references with your particular CSP nomenclature.

#### Create a secret with your platform details
Replace the password string (`YWRtaW4=`) with a base64 encoded version of your password and replace the `backend` with the IP address of the CSP backend and save it as `secret.yaml`:

```
apiVersion: v1
kind: Secret
metadata:
  name: nimble-secret
  namespace: kube-system
stringData:
  serviceName: nimble-csp-svc
  servicePort: "8080"
  backend: 192.168.1.1
  username: admin
data:
  # echo -n "admin" | base64
  password: YWRtaW4=
```

Create the secret using `kubectl`:
```
kubectl create -f secret.yaml
secret "nimble-secret" created
```

You should now see the `nimble-secret` in the `kube-system` namespace:
```
kubectl -n kube-system get secret/nimble-secret
NAME                  TYPE                                  DATA      AGE
nimble-secret         Opaque                                5         149m
```

#### Deploy the CSI driver and sidecars for the relevant Kubernetes version
Deployment declarations are stored in [hpe-storage/co-deployments](https://github.com/hpe-storage/co-deployments).

```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/hpe-linux-config.yaml
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/nimble-csp.yaml
```

**Note**: `nimble-csp.yaml` and `hpe-linux-config.yaml` are common for all kubernetes version.

Kubernetes 1.13
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/hpe-csi-k8s-1.13.yaml
```

Kubernetes 1.14
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/hpe-csi-k8s-1.14.yaml
```

Kubernetes 1.15
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/hpe-csi-k8s-1.15.yaml
```

Kubernetes 1.16
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/hpe-csi-k8s-1.16.yaml
```

Kubernetes 1.17
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/v1.0.0/hpe-csi-k8s-1.17.yaml
```

Depending on which version being deployed, different API objects gets created.

## Using the HPE CSI Driver for Kubernetes
Getting started with the HPE CSI Driver, setting up `StorageClass` and `VolumeSnapshotClass` API objects differs between CSP implementations. See [USING.md](USING.md) for examples to use the HPE Nimble Storage CSP.

**Note**:Support for `VolumeSnapshotClass` is available from Kubernetes 1.17+. The Snapshot Beta CRDs and the Common Snapshot Controller needs to be created as below

Install Snapshot Beta CRDs (Do this once per cluster)
```
kubectl create  -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
kubectl create  -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
kubectl create  -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml
```

Install Common Snapshot Controller (Do this once per cluster)
```
kubectl create -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
kubectl create -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml
```

## StorageClass parameters
The supported `StorageClass` parameters are dictated by the CSP from which the CSI Driver interacts with.
* [HPE Nimble Storage CSP](examples/kubernetes/hpe-nimble-storage/README.md)

Common CSI Driver parameters regardless of CSP:

Kubernetes 1.13:

```markdown

csi.storage.k8s.io/fstype: xfs
csi.storage.k8s.io/provisioner-secret-name: nimble-secret
csi.storage.k8s.io/provisioner-secret-namespace: kube-system
csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
csi.storage.k8s.io/node-stage-secret-name: nimble-secret
csi.storage.k8s.io/node-stage-secret-namespace: kube-system
csi.storage.k8s.io/node-publish-secret-name: nimble-secret
csi.storage.k8s.io/node-publish-secret-namespace: kube-system
fsMode: "0644"
fsOwner: "504:21"
accessProtocol: "iscsi"
```

Kubernetes 1.14 (Alpha feature: Volume Expansion):

```markdown

csi.storage.k8s.io/fstype: xfs
csi.storage.k8s.io/provisioner-secret-name: nimble-secret
csi.storage.k8s.io/provisioner-secret-namespace: kube-system
csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
csi.storage.k8s.io/node-stage-secret-name: nimble-secret
csi.storage.k8s.io/node-stage-secret-namespace: kube-system
csi.storage.k8s.io/node-publish-secret-name: nimble-secret
csi.storage.k8s.io/node-publish-secret-namespace: kube-system
csi.storage.k8s.io/resizer-secret-name: nimble-secret
csi.storage.k8s.io/resizer-secret-namespace: kube-system
fsMode: "0644"
fsOwner: "504:21"
accessProtocol: "iscsi"
```

Kubernetes 1.15 (Alpha features: PVC Cloning and Pod Inline Volume):

```markdown

csi.storage.k8s.io/fstype: xfs
csi.storage.k8s.io/provisioner-secret-name: nimble-secret
csi.storage.k8s.io/provisioner-secret-namespace: kube-system
csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
csi.storage.k8s.io/node-stage-secret-name: nimble-secret
csi.storage.k8s.io/node-stage-secret-namespace: kube-system
csi.storage.k8s.io/node-publish-secret-name: nimble-secret
csi.storage.k8s.io/node-publish-secret-namespace: kube-system
csi.storage.k8s.io/controller-expand-secret-name: nimble-secret
csi.storage.k8s.io/controller-expand-secret-namespace: kube-system
fsMode: "0644"
fsOwner: "504:21"
accessProtocol: "iscsi"
```

## Building the HPE CSI Driver

Instructions on how to build the HPE CSI Driver can be found in [BUILDING.md](BUILDING.md)

## Logging and Diagnostic

Log files associated with the HPE CSI Driver logs data to the standard output stream. If the logs need to be retained for long term, use a standard logging solution. Some of the logs on the host are persisted which follow standard logrotate policies.

### CSI Driver Logs

* Node Driver:
  `kubectl logs -f  daemonset.apps/hpe-csi-node  hpe-csi-driver -n kube-system`

* Controller Driver:
   `kubectl logs -f deployment.apps/hpe-csi-controller hpe-csi-driver -n kube-system`

**Note:** The logs for both Node and Controller Drivers are persisted at `/var/log/hpe-csi.log`

### Log Level

Log levels for both CSI Controller and Node driver can be controlled using `LOG_LEVEL` environment variable. Possible values are `info`, `warn`, `error`, `debug`, and `trace`. Apply the changes using `kubectl apply -f <yaml>` command after adding this to CSI controller and node container spec. For Helm charts this is controlled through `logLevel` variable in `values.yaml`.

### Container Service Provider Logs

CSP logs can be accessed as

`kubectl logs -f svc/nimble-csp-svc -n kube-system`

### Log Collector

Log collector script `hpe-logcollector.sh`can be used to collect the logs from any node which has kubectl access to the cluster.

```markdown
 curl -O https://raw.githubusercontent.com/hpe-storage/csi-driver/master/hpe-logcollector.sh
 chmod 555 hpe-logcollector.sh
```

Usage

```markdown
./hpe-logcollector.sh -h
Diagnostic Script to collect HPE Storage logs using kubectl

Usage:
     hpe-logcollector.sh [-h|--help][--node-name NODE_NAME][-n|--namespace NAMESPACE][-a|--all]
Where
-h|--help                  Print the Usage text
--node-name NODE_NAME      where NODE_NAME is kubernetes Node Name needed to collect the
                           hpe diagnostic logs of the Node
-n|--namespace NAMESPACE   where NAMESPACE is namespace of the pod deployment. default is kube-system
-a|--all                   collect diagnostic logs of all the nodes.If
                           nothing is specified logs would be collected
                           from all the nodes
```

## Support

The HPE CSI Driver 1.0 is fully supported and is Generally Available. Other features like volume expansion, raw block volumes, CSI snapshots are considered beta (Do not use these features for production and do not contact HPE for support). Refer to the [official table](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) of feature gates in the Kubernetes docs to find availability of beta features. Please file any issues, questions or feature requests [here](https://github.com/hpe-storage/csi-driver/issues). You may also join our Slack community to chat with HPE folks close to this project. We hang out in `#NimbleStorage` and `#Kubernetes` at [slack.hpedev.io](https://slack.hpedev.io/).

## Contributing

We value all feedback and contributions. If you find any issues or want to contribute, please feel free to open an issue or file a PR. More details in [CONTRIBUTING.md](CONTRIBUTING.md)

## License

This is open source software licensed using the Apache License 2.0. Please see [LICENSE](LICENSE) for details.
