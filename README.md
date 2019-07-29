A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for HPE Storage. The CSI driver allows you to use HPE Storage with K8S 1.12.

## Installing to Kubernetes

**Requirements:**

* `--allow-privileged` flag must be set to true for both the API server and the kubelet
* `--feature-gates=VolumeSnapshotDataSource=true,CSINodeInfo=true,CSIDriverRegistry=true` feature gate flags must be set to true for both the API server and the kubelet

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

#### 2. Deploy the CSI plugin and sidecars:

```
$ kubectl create -f csi-hpe-v0.3.0.yaml
```

#### 3. Test and verify volume provisioning:

Create a StorageClass referencing the secret and defining additional volume parameters:

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

**Storage Class Properties**

A storage class is used to create or clone an HPE Nimble Storage-backed persistent volume.  It can also be used to import an existing HPE Nimble Storage volume or clone of a snapshot into the Kubernetes cluster.  The parameters are grouped below by workflow.

#### Creation parameters

* fstype: ext4
  * The filesystem to create on the volume.
* csiProvisionerSecretName: nimble-secret
  * The name of the secret to use when provisioning a volume.
* csiProvisionerSecretNamespace: default
  * The namespace of the secret to use when provisioning a volume.
* csiControllerPublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiControllerPublishSecretNamespace: default
  * The namespace of the secret to use when publishing a volume to a node.
* csiNodeStageSecretName: nimble-secret
  * The name of the secret to use when staging a volume to a node.
* csiNodeStageSecretNamespace: default
  * The namespace of the secret to use when staging a volume to a node.
* csiNodePublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiNodePublishSecretName: default
  * The namespace of the secret to use when publishing a volume to a node.
* fsOwner
  * The user id and group id that should own the root directory of the filesystem in the form of [userId:groupId].
* fsMode
  * 1 to 4 octal digits that represent the file mode to be applied to the root directory of the filesystem.
* limitIops
  * The IOPS limit of the volume.  The IOPS limit should be in the range [256, 4294967294], or -1 for unlimited.
* limitMbps
  * The MB/s throughput limit for the volume.
* ephemeral
  * "true" or "false". Indicates the backing Nimble volume (including snapshots) should be destroyed when the PVC is deleted.
* description
  * Text to be added to the volume's description.
* performancePolicy
  * The name of the performance policy to assign to the volume.
  * Example performance policies include "Backup Repository", "Exchange 2003 data store", "Exchange 2007 data store", "Exchange 2010 data store", "Exchange log", "Oracle OLTP", "Other Workloads", "SharePoint", "SQL Server", "SQL Server 2012", "SQL Server Logs".
* protectionTemplate
  * The name of the protection template to assign to the volume.
  * Examples of protection templates include "Retain-30Daily", "Retain-48Hourly-30aily-52Weekly", and "Retain-90Daily".
* pool
  * The name of the pool in which to place the volume.
* folder
  * The name of the folder in which to place the volume.
* encrypted
  * "true" or "false". Indicates that the volume should be encrypted (dedupeEnabled and encrypted are mutually exclusive).
* thick
  * "true" or "false". Indicates that the volume should be thick provisioned (dedupeEnabled and thick are mutually exclusive).
* dedupeEnabled
  * "true" or "false". Indicates that the volume should enable deduplication (dedupeEnabled and thick are mutually exclusive).
* syncOnDetach
  * "true" or "false". Indicates that a snapshot of the volume should be synced to the replication partner each time it is detached from a node.
* volumeNameSuffix
  * A suffix to add to the end of each volume name.

#### Clone parameters

* csiProvisionerSecretName: nimble-secret
  * The name of the secret to use when provisioning a volume.
* csiProvisionerSecretNamespace: default
  * The namespace of the secret to use when provisioning a volume.
* csiControllerPublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiControllerPublishSecretNamespace: default
  * The namespace of the secret to use when publishing a volume to a node.
* csiNodeStageSecretName: nimble-secret
  * The name of the secret to use when staging a volume to a node.
* csiNodeStageSecretNamespace: default
  * The namespace of the secret to use when staging a volume to a node.
* csiNodePublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiNodePublishSecretName: default
  * The namespace of the secret to use when publishing a volume to a node.
* cloneOf
  * The name of the PVC to be cloned.
* snapshot
  * The name of the snapshot to base the clone on.  This is optional.  If not specified, a new snapshot is created.
* createSnapshot
  * "true" or "false". Indicates that a new snapshot of the volume should be taken matching the name provided in the snapshot parameter.  If the snapshot parameter is not specified, a default name will be created.
* limitIops
  * The IOPS limit of the volume.  The IOPS limit should be in the range [256, 4294967294], or -1 for unlimited.
* limitMbps
  * The MB/s throughput limit for the volume.
* ephemeral
  * "true" or "false". Indicates the backing Nimble volume (including snapshots) should be destroyed when the PVC is deleted.
* description
  * Text to be added to the volume's description.
* performancePolicy
  * The name of the performance policy to assign to the volume.
  * Note that changing the performance policy of a clone cannot occur if the block size or application category changes.
* protectionTemplate
  * The name of the protection template to assign to the volume.
  * Examples of protection templates include "Retain-30Daily", "Retain-48Hourly-30aily-52Weekly", and "Retain-90Daily".
* thick
  * "true" or "false". Indicates that the volume should be thick provisioned (dedupeEnabled and thick are mutually exclusive).
* dedupeEnabled
  * "true" or "false". Indicates that the volume should enable deduplication (dedupeEnabled and thick are mutually exclusive).
* syncOnDetach
  * "true" or "false". Indicates that a snapshot of the volume should be synced to the replication partner each time it is detached from a node.
* volumeNameSuffix
  * A suffix to add to the end of each volume name.
* snapshotNamePrefx
  * A prefix to add to the beginning of the snapshot name.

#### Import clone of snapshot parameters

* csiProvisionerSecretName: nimble-secret
  * The name of the secret to use when provisioning a volume.
* csiProvisionerSecretNamespace: default
  * The namespace of the secret to use when provisioning a volume.
* csiControllerPublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiControllerPublishSecretNamespace: default
  * The namespace of the secret to use when publishing a volume to a node.
* csiNodeStageSecretName: nimble-secret
  * The name of the secret to use when staging a volume to a node.
* csiNodeStageSecretNamespace: default
  * The namespace of the secret to use when staging a volume to a node.
* csiNodePublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiNodePublishSecretName: default
  * The namespace of the secret to use when publishing a volume to a node.
* importVolAsClone
  * The name of the Nimble volume to clone and import.
* snapshot
  * The name of the Nimble snapshot to clone and import.  This is optional.  If not specified, a new snapshot is created.
* createSnapshot
  * "true" or "false". Indicates that a new snapshot of the volume should be taken matching the name provided in the snapshot parameter.  If the snapshot parameter is not specified, a default name will be created.
* limitIops
  * The IOPS limit of the volume.  The IOPS limit should be in the range [256, 4294967294], or -1 for unlimited.
* limitMbps
  * The MB/s throughput limit for the volume.
* ephemeral
  * "true" or "false". Indicates the backing Nimble volume (including snapshots) should be destroyed when the PVC is deleted.
* description
  * Text to be added to the volume's description.
* performancePolicy
  * The name of the performance policy to assign to the volume.
  * Note that changing the performance policy of a clone cannot occur if the block size or application category changes.
* protectionTemplate
  * The name of the protection template to assign to the volume.
  * Examples of protection templates include "Retain-30Daily", "Retain-48Hourly-30aily-52Weekly", and "Retain-90Daily".
* thick
  * "true" or "false". Indicates that the volume should be thick provisioned (dedupeEnabled and thick are mutually exclusive).
* dedupeEnabled
  * "true" or "false". Indicates that the volume should enable deduplication (dedupeEnabled and thick are mutually exclusive).
* syncOnDetach
  * "true" or "false". Indicates that a snapshot of the volume should be synced to the replication partner each time it is detached from a node.
* volumeNameSuffix
  * A suffix to add to the end of each volume name.
* snapshotNamePrefx
  * A prefix to add to the beginning of the snapshot name.

#### Import volume parameters

* csiProvisionerSecretName: nimble-secret
  * The name of the secret to use when provisioning a volume.
* csiProvisionerSecretNamespace: default
  * The namespace of the secret to use when provisioning a volume.
* csiControllerPublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiControllerPublishSecretNamespace: default
  * The namespace of the secret to use when publishing a volume to a node.
* csiNodeStageSecretName: nimble-secret
  * The name of the secret to use when staging a volume to a node.
* csiNodeStageSecretNamespace: default
  * The namespace of the secret to use when staging a volume to a node.
* csiNodePublishSecretName: nimble-secret
  * The name of the secret to use when publishing a volume to a node.
* csiNodePublishSecretName: default
  * The namespace of the secret to use when publishing a volume to a node.
* importVolumeName
  * The name of the Nimble volume to import.
* snapshot
  * The name of the Nimble snapshot to restore the imported volume to after takeover.  If not specified, the volume will not be restored.
* takeover
  * "true" or "false". Indicates the current group will takeover ownership of the Nimble volume and volume collection.  This should be performed against a downstream replica.
* reverseReplication
  * "true" or "false". Reverses the replication direction so that writes to the Nimble volume are replicated back to the group where it was replicated from.
* forceImport
  * "true" or "false". Forces the import of a volume that is not owned by the group and is not part of a volume collection.  If the volume is part of a volume collection, use takeover instead.
