# csi-driver

A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for HPE Storage. The CSI driver allows you to use HPE Storage with your preferred container orchestrator.

## Installing to Kubernetes

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

* k8s 1.12
  * `--allow-privileged` flag must be set to true for both the API server and the kubelet
  * `--feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,VolumeSnapshotDataSource=true` feature gate flags must be set to true for both the API server and the kubelet
* k8s 1.13
  * `--allow-privileged` flag must be set to true for the API server
  * `--feature-gates=VolumeSnapshotDataSource=true` feature gate flags must be set to true for the API server
* k8s 1.14
  * `--allow-privileged` flag must be set to true for the API server
  * `--feature-gates=ExpandCSIVolumes=true,ExpandInUsePersistentVolumes=true` feature gate flags must be set to true for both the API server and kubelet.

#### 1. Create a secret with your array details:

Replace the password string (`YWRtaW4=`) with a base64 encoded version of your password and replace the arrayIp with your array IP address and save it as `secret.yaml`:

```
apiVersion: v1
kind: Secret
metadata:
  name: nimble-secret
  namespace: default
stringData:
  serviceName: csp-service
  arrayIp: 10.10.10.1
  port: "8080"
  username: admin
data:
  # echo -n "admin" | base64
  password: YWRtaW4=
```

and create the secret using kubectl:

```
$ kubectl create -f secret.yaml
secret "nimble-secret" created
```

You should now see the nimble secret in the `default` namespace along with other secrets

```
$ kubectl get secret
NAME                  TYPE                                  DATA      AGE
default-token-jxx2s   kubernetes.io/service-account-token   3         2d1h
nimble-secret         Opaque                                5         149m
```

#### 2. Deploy the CSI plugin and sidecars for the relevant k8s version:

* k8s 1.12
```
$ kubectl create -f csi-hpe-v0.3.0.yaml
```

* k8s 1.13
```
$ kubectl create -f csi-hpe-v1.0.0.yaml
```

* k8s 1.14
```
$ kubectl create -f csi-hpe-v1.1.0.yaml
```

#### 3. Test and verify volume provisioning:

Create a StorageClass referencing the secret and defining additional volume parameters:

* k8s 1.12
```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc-csi-1
provisioner: csi.hpe.com
parameters:
  fstype: ext4
  csiProvisionerSecretName: nimble-secret
  csiProvisionerSecretNamespace: default
  csiControllerPublishSecretName: nimble-secret
  csiControllerPublishSecretNamespace: default
  csiNodeStageSecretName: nimble-secret
  csiNodeStageSecretNamespace: default
  csiNodePublishSecretName: nimble-secret
  csiNodePublishSecretNamespace: default
  description: "Volume from csi"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
```

* k8s 1.13+
```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc-csi-1
provisioner: csi.hpe.com
parameters:
  csi.storage.k8s.io/fstype: ext4
  csi.storage.k8s.io/provisioner-secret-name: nimble-secret
  csi.storage.k8s.io/provisioner-secret-namespace: default
  csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
  csi.storage.k8s.io/controller-publish-secret-namespace: default
  csi.storage.k8s.io/node-stage-secret-name: nimble-secret
  csi.storage.k8s.io/node-stage-secret-namespace: default
  csi.storage.k8s.io/node-publish-secret-name: nimble-secret
  csi.storage.k8s.io/node-publish-secret-namespace: default
  description: "Volume from csi"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
```

Create a PersistentVolumeClaim. This makes sure a volume is created and provisioned on your behalf:

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-csi-1
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: sc-csi-1
```

Check that a new `PersistentVolume` is created based on your claim:

```
$ kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM               STORAGECLASS   REASON   AGE
pvc-13336da3-7ca3-11e9-826c-00505693581f   10Gi       RWO            Delete           Bound    default/pvc-csi-1   sc-csi-1                3s
```

The above output means that the CSI driver successfully provisioned a new volume.  The volume is not attached to any node yet. It will only be attached to a node if a workload is scheduled to a specific node. Now let us create a Pod that refers to the above volume. When the Pod is created, the volume will be attached, formatted and mounted to the specified container:

```
kind: Pod
apiVersion: v1
metadata:
  name: pod-csi-1
spec:
  containers:
    - name: pod-csi-cont-1
      image: nginx
      command: ["bin/sh"]
      args: ["-c", "while true; do date >> /data/mydata.txt; sleep 1; done"]
      volumeMounts:
        - name: export1
          mountPath: /data
    - name: pod-csi-cont-2
      image: debian
      command: ["bin/sh"]
      args: ["-c", "while true; do date >> /data/mydata.txt; sleep 1; done"]
      volumeMounts:
        - name: export1
          mountPath: /data
  volumes:
    - name: export1
      persistentVolumeClaim:
        claimName: pvc-csi-1
```

Check if the pod is running successfully:

```
$ kubectl get pod pod-csi-1
NAME        READY   STATUS    RESTARTS   AGE
pod-csi-1   2/2     Running   0          2m29s
```

#### 4. Test and verify snapshots:

Create a VolumeStorageClass referencing the secret and defining additional snapshot parameters:

* k8s 1.12
```
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshotClass
metadata:
  name: snapclass-csi-1
snapshotter: csi.hpe.com
parameters:
  description: "Snapshot from CSI"
  csiSnapshotterSecretName: nimble-secret
  csiSnapshotterSecretNamespace: default
```

* k8s 1.13+
```
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshotClass
metadata:
  name: snapclass-csi-1
snapshotter: csi.hpe.com
parameters:
  description: "Snapshot from CSI"
  csi.storage.k8s.io/snapshotter-secret-name: nimble-secret
  csi.storage.k8s.io/snapshotter-secret-namespace: default
```

Create a VolumeSnapshot. This will create a new snapshot of the volume

```
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshot
metadata:
  name: snap-csi-1
spec:
  source:
    name: pvc-csi-1
    kind: PersistentVolumeClaim
  snapshotClassName: snapclass-csi-1
```

Check that a new `VolumeSnapshot` is created based on your claim:

```
$ kubectl describe volumesnapshot snap-csi-1
[root@gcostea-k8s3 ~]# kubectl describe volumesnapshot snap-csi-1
Name:         snap-csi-1
Namespace:    default
...
Status:
  Creation Time:  2019-05-22T15:51:28Z
  Ready:          true
  Restore Size:   10Gi
```

#### 5. Test and verify resize:

To perform resize operations on k8s 1.14, you must enhance your storage class with some additional attributes:

```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc-csi-1
provisioner: csi.hpe.com
parameters:
  csi.storage.k8s.io/fstype: ext4
  csi.storage.k8s.io/provisioner-secret-name: nimble-secret
  csi.storage.k8s.io/provisioner-secret-namespace: default
  csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
  csi.storage.k8s.io/controller-publish-secret-namespace: default
  csi.storage.k8s.io/node-stage-secret-name: nimble-secret
  csi.storage.k8s.io/node-stage-secret-namespace: default
  csi.storage.k8s.io/node-publish-secret-name: nimble-secret
  csi.storage.k8s.io/node-publish-secret-namespace: default
  csi.storage.k8s.io/resizer-secret-name: nimble-secret
  csi.storage.k8s.io/resizer-secret-namespace: default
  description: "Volume from csi"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
  allowVolumeExpansion: true
```

#### 6. Test and verify Persistent Volume Claim overrides:

To allow Persistent Volume Claim (PVC) to override the storage class parameters.
Define the parameters allowed to be overridden in the storage class
Now, override these parameters as annotation in the PVC annotations field as defined below.


```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc-csi-1
provisioner: csi.hpe.com
parameters:
  csi.storage.k8s.io/fstype: xfs
  csi.storage.k8s.io/provisioner-secret-name: nimble-secret
  csi.storage.k8s.io/provisioner-secret-namespace: default
  csi.storage.k8s.io/controller-publish-secret-name: nimble-secret
  csi.storage.k8s.io/controller-publish-secret-namespace: default
  csi.storage.k8s.io/node-stage-secret-name: nimble-secret
  csi.storage.k8s.io/node-stage-secret-namespace: default
  csi.storage.k8s.io/node-publish-secret-name: nimble-secret
  csi.storage.k8s.io/node-publish-secret-namespace: default
  description: "Volume from csi"
  dedupeEnabled: "false"
  performancePolicy: "SQL Server"
  limitIops: "76800"
  allowOverrides: description,limitIOPS,performancePolicy
```


```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
 name: pv-csi-1
 annotations:
    csi.hpe.com/description: "This is my custom description"
    csi.hpe.com/limitIOPS: "8000"
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: sc-csi-1
```

## Sample storage classes

Sample storage classes can be found for [Nimble Storage](examples/kubernetes/nimble-storage/README.md), Simplivity, and 3Par.

## Contributing

We value all feedback and contributions.  If you find any issues or want to contribute, please feel free to open an issue or file a PR.