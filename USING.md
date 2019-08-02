## Using the HPE CSI Driver
These instructions are provided as an example on how to use the HPE CSI Driver with the HPE Nimble Storage CSP.

### Test and verify volume provisioning
The below YAML declarations are meant to be created with `kubectl create`. Either copy the content to a file on the host where `kubectl` is being executed, or copy & paste into the terminal, like this:

```
kubectl create -f-
< paste the YAML >
^D (CTRL + D)
```

**Tip:** Some of these example declarations are also available in the [examples/kubernetes](examples/kubernetes) directory and all the HPE Nimble Storage CSP `StorageClass` parameters can we found in [examples/kubernetes/hpe-nimble-storage](examples/kubernetes/hpe-nimble-storage).

To get started, create a `StorageClass` API object referencing the `nimble-secret` and defining additional (optional) `StorageClass` parameters:

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

Create a `PersistentVolumeClaim`. This makes sure a volume is created and provisioned on your behalf, make sure to reference the correct `.spec.storageClassName`:

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

**Note:** In most enviornments, there is a default `StorageClass` declared on the cluster. In such a scenario, the `.spec.storageClassName` can be omitted. In addition, the default `StorageClass` needs to be annotated with this key: `.metadata.annotations.storageclass.kubernetes.io/is-default-class` set to `"true"`. 

After the PVC has been declared, check that a new `PersistentVolume` is created based on your claim:

```
$ kubectl get pv
NAME              CAPACITY ACCESS MODES RECLAIM POLICY STATUS CLAIM            STORAGECLASS AGE
pvc-13336da3-7... 10Gi     RWO          Delete         Bound  default/my-pvc-1 my-sc-1      3s
```

The above output means that the HPE CSI Driver successfully provisioned a new volume. The volume is not attached to any node yet. It will only be attached to a node if a workload is scheduled requesting the PVC. Now let us create a `Pod` that refers to the above volume. When the `Pod` is created, the volume will be attached, formatted and mounted to the specified container:

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

The default `volumeMode` for the `PersistentVolumeClaim` is set to `Filesystem`. If a Raw Block Device is desired, `volumeMode` needs to be set to `Block`. Example:

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mv-raw-pvc-1
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: my-sc-1
  volumeMode: Block
```

Creating the PVC is identical to `volumeMode: Filesystem`, mapping the device in a `Pod` specification is slightly different as a `volumeDevices` section is added instead of a `volumeMounts` stanza:

```
apiVersion: v1
kind: Pod
metadata:
  name: my-raw-pvc-1
spec:
  containers:
    - name: my-null-pod
      image: fedora:31
      command: ["/bin/sh", "-c"]
      args: [ "tail -f /dev/null" ]
      volumeDevices:
        - name: data
          devicePath: /dev/xvda
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: my-raw-pvc-1
```

### Test and verify snapshots
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

### Test and verify resize
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

### Test and verify Persistent Volume Claim overrides
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

## Further reading
The [official Kubernetes documentation](https://kubernetes.io/docs/concepts/storage/volumes/) contains comprehensive documentation on how to markup `PersistentVolumeClaim` and `StorageClass` API objects to tweak certain behaviors.
