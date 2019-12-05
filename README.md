# HPE CSI Driver for Kubernetes
A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for Kubernetes. The HPE CSI Driver for Kubernetes allows you to use a [Container Storage Provider](https://github.com/hpe-storage/container-storage-provider) to perform data management operations on storage resources.

## Deploying to Kubernetes
This guide is primarily written to accommodate installation on upstream Kubernetes. Installation steps may vary for different distributions of Kubernetes. Please see the [hpe-storage/co-deployments](https://github.com/hpe-storage/co-deployments) for additional methods, tweaks and platforms.

### Kubernetes Compatibility

<table>
  <thead>
    <tr>
      <th></th>
      <th colspan=4>Kubernetes Version</th>
    </tr>
    <tr>
      <th>HPE CSI Driver</th>
      <th><= 1.11</th>
      <th>1.12+</th>
      <th>1.13+</th>
      <th>1.14+</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>v0.3.0-beta</td>
      <td>no</td>
      <td>yes</td>
      <td>no</td>
      <td>no</td>
    </tr>
    <tr>
      <td>v1.0.0-beta</td>
      <td>no</td>
      <td>no</td>
      <td>yes</td>
      <td>yes</td>
    </tr>
  </tbody>
</table>

### Requirements
Different features mature at different rates. Refer to the [official table](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) of feature gates in the Kubernetes docs.

The following guidelines applies to what feature gates got introduced as alphas for the corresponding version of K8s. For example, `ExpandCSIVolumes` got introduced in 1.14 but is still an alpha in 1.15, hence you need to enable that feature gate in 1.15 as well if you want to use it.

Kubernetes 1.12
 * `--allow-privileged` flag must be set to true for both the API server and the kubelet
 * `--feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,VolumeSnapshotDataSource=true` feature gate flags must be set to true for both the API server and the kubelet

Kubernetes 1.13
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=VolumeSnapshotDataSource=true` feature gate flag must be set to true for the API server for VolumeSnapshot cloning support

Kubernetes 1.14
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=VolumeSnapshotDataSource=true` feature gate flag must be set to true for the API server for VolumeSnapshot cloning support
 * `--feature-gates=ExpandCSIVolumes=true,ExpandInUsePersistentVolumes=true` feature gate flags must be set to true for both the API server and kubelet for resize support

Kubernetes 1.15
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=VolumeSnapshotDataSource=true` feature gate flag must be set to true for the API server for VolumeSnapshot cloning support
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

Kubernetes 1.12
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/nimble-csp.yaml
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/hpe-csi-k8s-1.12.yaml
```

Kubernetes 1.13
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/nimble-csp.yaml
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/hpe-csi-k8s-1.13.yaml
```

Kubernetes 1.14
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/nimble-csp.yaml
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/hpe-csi-k8s-1.14.yaml
```

Kubernetes 1.15
```
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/nimble-csp.yaml
kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/hpe-csi-k8s-1.15.yaml
```

Depending on which version being deployed, different API objects gets created.

## Using the HPE CSI Driver for Kubernetes
Getting started with the HPE CSI Driver, setting up `StorageClass` and `VolumeSnapshotClass` API objects differs between CSP implementations. See [USING.md](USING.md) for examples to use the HPE Nimble Storage CSP.

## StorageClass parameters
The supported `StorageClass` parameters are dictated by the CSP from which the CSI Driver interacts with.
* [HPE Nimble Storage CSP](examples/kubernetes/hpe-nimble-storage/README.md)

Common CSI Driver parameters regardless of CSP:

Kubernetes 1.12

```markdown

fstype: xfs
csiProvisionerSecretName: nimble-secret
csiProvisionerSecretNamespace: kube-system
csiControllerPublishSecretName: nimble-secret
csiControllerPublishSecretNamespace: kube-system
csiNodeStageSecretName: nimble-secret
csiNodeStageSecretNamespace: kube-system
csiNodePublishSecretName: nimble-secret
csiNodePublishSecretNamespace: kube-system
fsMode: "0644"
fsOwner: "504:21"
accessProtocol: "iscsi"
```

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

The HPE CSI Driver is considered beta software. Do not use for production and do not contact HPE for support. Please file any issues, questions or feature requests [here](https://github.com/hpe-storage/csi-driver/issues). You may also join our Slack community to chat with HPE folks close to this project. We hang out in `#NimbleStorage` and `#Kubernetes` at [slack.hpedev.io](https://slack.hpedev.io/).

## Contributing

We value all feedback and contributions. If you find any issues or want to contribute, please feel free to open an issue or file a PR. More details in [CONTRIBUTING.md](CONTRIBUTING.md)

## License

This is open source software licensed using the Apache License 2.0. Please see [LICENSE](LICENSE) for details.
