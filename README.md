# HPE CSI Driver for Kubernetes
A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for Kubernetes. The HPE CSI Driver for Kubernetes allows you to use a [Container Storage Provider](https://github.com/hpe-storage/container-storage-provider) to perform data management operations on storage resources.



## Installing to Kubernetes
This guide is primarily written to accommodate installation on upstream Kubernetes. Variances in the installation steps may vary for different distributions of Kubernetes. 

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

**Requirements:**

Kubernetes 1.12
 * `--allow-privileged` flag must be set to true for both the API server and the kubelet
 * `--feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,VolumeSnapshotDataSource=true` feature gate flags must be set to true for both the API server and the kubelet
 
Kubernetes 1.13
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=VolumeSnapshotDataSource=true` feature gate flags must be set to true for the API server
 
Kubernetes 1.14
 * `--allow-privileged` flag must be set to true for the API server
 * `--feature-gates=ExpandCSIVolumes=true,ExpandInUsePersistentVolumes=true` feature gate flags must be set to true for both the API server and kubelet

### Deploying the HPE CSI Driver for Kubernetes
The following example walks through deployment of the driver with a HPE Nimble Storage backend. Replace any Nimble references with your particular CSP nomenclature.

#### 1. Create a secret with your platform details
Replace the password string (`YWRtaW4=`) with a base64 encoded version of your password and replace the systemIp with the IP address of the backend and save it as `secret.yaml`:

```
apiVersion: v1
kind: Secret
metadata:
  name: nimble-secret
  namespace: default
stringData:
  serviceName: csp-service
  servicePort: "8080"
  systemIp: 10.10.10.1
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

#### 2. Deploy the CSI driver and sidecars for the relevant k8s version
Deployment declarations are stored in [hpe-storage/co-deployments](https://github.com/hpe-storage/co-deployments).

Kubernetes 1.12
`kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/csi-hpe-v0.3.0.yaml`

Kubernetes 1.13
`kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/csi-hpe-v1.0.0.yaml`

Kubernetes 1.14
`kubectl create -f https://raw.githubusercontent.com/hpe-storage/co-deployments/master/yaml/csi-driver/csi-hpe-v1.1.0.yaml`

Depending on which version being deployd, different API objects gets created.

#### 3. Test and verify volume provisioning
The below YAML declarations are meant to be created with `kubectl create`. Either copy the content to a file on the host `kubectl` is being executed, or copy & paste into the terminal, like this:

```
kubectl create -f-
< paste the YAML >
^D (CTRL + D)
```

Create a `StorageClass` API object referencing the `nimble-secret` and defining additional (optional) volume parameters:

Kubernetes 1.12
```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: my-sc-1
provisioner: csi.hpe.com
parameters:
  fstype: xfs
  csiProvisionerSecretName: nimble-secret
  csiProvisionerSecretNamespace: kube-system
  csiControllerPublishSecretName: nimble-secret
  csiControllerPublishSecretNamespace: kube-system
  csiNodeStageSecretName: nimble-secret
  csiNodeStageSecretNamespace: kube-system
  csiNodePublishSecretName: nimble-secret
  csiNodePublishSecretNamespace: kube-system
  description: "Volume provisioned by the HPE CSI Driver"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
```

Kubernetes 1.13+
```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: my-sc-1
provisioner: csi.hpe.com
parameters:
  csi.storage.k8s.io/fstype: xfs
  csi.storage.k8s.io/provisioner-secret-name: nimble-secret
  csi.storage.k8s.io/provisioner-secret-namespace: kube-system
  csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
  csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
  csi.storage.k8s.io/node-stage-secret-name: nimble-secret
  csi.storage.k8s.io/node-stage-secret-namespace: kube-system
  csi.storage.k8s.io/node-publish-secret-name: nimble-secret
  csi.storage.k8s.io/node-publish-secret-namespace: kube-system
  description: "Volume provisioned by the HPE CSI Driver"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
```

Create a `PersistentVolumeClaim`. This makes sure a volume is created and provisioned on your behalf, make sure to reference the correct `StorageClassName`:

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc-1
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: my-sc-1
```

Check that a new `PersistentVolume` is created based on your claim:

```
$ kubectl get pv
NAME              CAPACITY ACCESS MODES RECLAIM POLICY STATUS CLAIM            STORAGECLASS AGE
pvc-13336da3-7... 10Gi     RWO          Delete         Bound  default/my-pvc-1 my-sc-1      3s
```

The above output means that the HPE CSI Driver successfully provisioned a new volume.  The volume is not attached to any node yet. It will only be attached to a node if a workload is scheduled to a specific node. Now let us create a `Pod` that refers to the above volume. When the `Pod` is created, the volume will be attached, formatted and mounted to the specified container:

```
kind: Pod
apiVersion: v1
metadata:
  name: my-pod-1
spec:
  containers:
    - name: pod-datelog-1
      image: nginx
      command: ["bin/sh"]
      args: ["-c", "while true; do date >> /data/mydata.txt; sleep 1; done"]
      volumeMounts:
        - name: export1
          mountPath: /data
    - name: pod-datelog-2
      image: debian
      command: ["bin/sh"]
      args: ["-c", "while true; do date >> /data/mydata.txt; sleep 1; done"]
      volumeMounts:
        - name: export1
          mountPath: /data
  volumes:
    - name: export1
      persistentVolumeClaim:
        claimName: my-pvc-1
```

Check if the `Pod` is running successfully:
```
$ kubectl get pod pod-csi-1
NAME        READY   STATUS    RESTARTS   AGE
my-pod-1    2/2     Running   0          2m29s
```

**Note:** A simple `Pod` does not provide any automatic recovery if the node the `Pod` is scheduled on crashes. Please see [the official Kubernetes documentation](https://kubernetes.io/docs/concepts/workloads/) for different workload types that provide automatic recovery. A shortlist of recommended workload types that are suitable for persistent storage is available in [this blog post](https://datamattsson.tumblr.com/post/182297931146/highly-available-stateful-workloads-on-kubernetes) and best practices are outlined in [this blog post](https://datamattsson.tumblr.com/post/185031432701/best-practices-for-stateful-workloads-on).

#### 4. Test and verify snapshots:
Create a `VolumeStorageClass` referencing the `nimble-secret` and defining additional snapshot parameters:

Kubernetes 1.12
```
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshotClass
metadata:
  name: my-snapclass-1
snapshotter: csi.hpe.com
parameters:
  description: "Snapshot created by the HPE CSI Driver"
  csiSnapshotterSecretName: nimble-secret
  csiSnapshotterSecretNamespace: kube-system
```

Kubernetes 1.13+
```
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshotClass
metadata:
  name: my-snapclass-1
snapshotter: csi.hpe.com
parameters:
  description: "Snapshot created by the HPE CSI Driver"
  csi.storage.k8s.io/snapshotter-secret-name: nimble-secret
  csi.storage.k8s.io/snapshotter-secret-namespace: kube-system
```

Create a `VolumeSnapshot`. This will create a new snapshot of the volume
```
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshot
metadata:
  name: my-snapshot-1
spec:
  source:
    name: my-pvc-1
    kind: PersistentVolumeClaim
  snapshotClassName: my-snapclass-1
```

Check that a new `VolumeSnapshot` is created based on your claim:
```
kubectl describe volumesnapshot my-snapshot-1
Name:         my-snapshot-1
Namespace:    default
...
Status:
  Creation Time:  2019-05-22T15:51:28Z
  Ready:          true
  Restore Size:   10Gi
```

#### 5. Test and verify resize:
To perform resize operations on Kubernetes 1.14, you must enhance your `StorageClass` with some additional attributes (`allowVolumeExpansion` and a secret for the resizer sidecar):

```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: my-sc-1
provisioner: csi.hpe.com
parameters:
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
  description: "Volume provisioned by the HPE CSI Driver"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
  allowVolumeExpansion: true
```

A volume provisioned by the above `StorageClass` may now be resized by editing the `.spec.resources.requests.storage` attribute of the `PersistentVolumeClaim`.

#### 6. Test and verify Persistent Volume Claim overrides:
The HPE CSI Driver allows the `PersistentVolumeClaim` to override the `StorageClass` parameters by annotating the `PersistentVolumeClaim`. Define the parameters allowed to be overridden in the `StorageClass` by setting the `allowOverrides` parameter:

```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: my-sc-1
provisioner: csi.hpe.com
parameters:
  csi.storage.k8s.io/fstype: xfs
  csi.storage.k8s.io/provisioner-secret-name: nimble-secret
  csi.storage.k8s.io/provisioner-secret-namespace: kube-system
  csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
  csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
  csi.storage.k8s.io/node-stage-secret-name: nimble-secret
  csi.storage.k8s.io/node-stage-secret-namespace: kube-system
  csi.storage.k8s.io/node-publish-secret-name: nimble-secret
  csi.storage.k8s.io/node-publish-secret-namespace: kube-system
  description: "Volume provisioned by the HPE CSI Driver"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
  allowOverrides: description,limitIOPS,performancePolicy
```

The end-user may now control those parameters (the `StorageClass` provide the default values):
```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
 name: my-pvc-1
 annotations:
    csi.hpe.com/description: "This is my custom description"
    csi.hpe.com/limitIOPS: "8000"
    csi.hpe.com/performancePolicy: "MariaDB"
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: my-sc-1
```

## StorageClass parameters
The supported `StorageClass` parameters are dictated by the CSP the CSI Driver interacts with.
* [HPE Nimble Storage](examples/kubernetes/hpe-nimble-storage/README.md)

Common CSI Driver parameters regardless of CSP:

Kubernetes 1.12
```
fstype: xfs
csiProvisionerSecretName: nimble-secret
csiProvisionerSecretNamespace: kube-system
csiControllerPublishSecretName: nimble-secret
csiControllerPublishSecretNamespace: kube-system
csiNodeStageSecretName: nimble-secret
csiNodeStageSecretNamespace: kube-system
csiNodePublishSecretName: nimble-secret
csiNodePublishSecretNamespace: kube-system
```

Kubernetes 1.13 (resizer is 1.14 only):
```
csi.storage.k8s.io/fstype: ext4
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
```

## Support
The HPE CSI Driver is considered beta software. Do not use for production and do not contact HPE for support. Please file [issues here](issues).

## Contributing
We value all feedback and contributions. If you find any issues or want to contribute, please feel free to open an issue or file a PR. More details in [CONTRIBUTING.md](CONTRIBUTING.md)

## License
This is open source software licensed using the Apache License 2.0. Please see [LICENSE.md](LICENSE.md) for details.
