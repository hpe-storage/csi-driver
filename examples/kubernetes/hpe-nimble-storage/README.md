## HPE Nimble Storage StorageClass parameters for the HPE CSI Driver
A `StorageClass` is used to provision or clone an HPE Nimble Storage-backed persistent volume. It can also be used to import an existing HPE Nimble Storage volume or clone of a snapshot into the Kubernetes cluster. The parameters are grouped below by those same workflows.

A sample [storage-class.yaml](storage-class.yaml) is provided.

Backward compatibility with the HPE Nimble Storage FlexVolume Driver is being honored to a certain degree. `StorageClass` API objects needs be rewritten and parameters need to be updated regardless.

**Note:** These are optional parameters.

### Common parameters for Provisioning and Cloning
These parameters are mutable betweeen a parent volume and creating a clone from a snapshot.

| Parameter | String | Description |
| --------- | ------ | ----------- |
| accessProtocol | Text | The access protocol to use when accessing the persistent volume.  Defaults to "iscsi" when unspecified. |
| destroyOnDelete | Boolean | Indicates the backing Nimble volume (including snapshots) should be destroyed when the PVC is deleted. |
| limitIops | Integer | The IOPS limit of the volume. The IOPS limit should be in the range 256 to 4294967294, or -1 for unlimited (default). |
| limitMbps | Integer | The MB/s throughput limit for the volume. |
| description | Text | Text to be added to the volume's description on the Nimble array. |
| performancePolicy | Text | The name of the performance policy to assign to the volume. Default example performance policies include "Backup Repository", "Exchange 2003 data store", "Exchange 2007 data store", "Exchange 2010 data store", "Exchange log", "Oracle OLTP", "Other Workloads", "SharePoint", "SQL Server", "SQL Server 2012", "SQL Server Logs". |
| protectionTemplate | Text | The name of the protection template to assign to the volume. Default examples of protection templates include "Retain-30Daily", "Retain-48Hourly-30aily-52Weekly", and "Retain-90Daily". |
| folder | Text | The name of the Nimble folder in which to place the volume. |
| thick | Boolean | Indicates that the volume should be thick provisioned. |
| dedupeEnabled | Boolean | Indicates that the volume should enable deduplication. |
| syncOnDetach | Boolean | Indicates that a snapshot of the volume should be synced to the replication partner each time it is detached from a node. |

**Note**: Performance Policies, Folders and Protection Templates are Nimble specific constructs that can be created on the Nimble array itself to address particular requirements or workloads. Please consult with the storage admin or read the admin guide found on [HPE InfoSight](https://infosight.hpe.com).

### Provisioning parameters
These parameters are immutable for clones once a volume has been created.

| Parameter | String | Description |
| --------- | ------ | ----------- |
| fsOwner | userId:groupId | The user id and group id that should own the root directory of the filesystem. |
| fsMode | Octal digits | 1 to 4 octal digits that represent the file mode to be applied to the root directory of the filesystem. |
| fsCreateOptions | Text | A string to be passed to the mkfs command.  These flags are opaque to CSI and are therefore not validated.  To protect the node, only the following characters are allowed:  ```[a-zA-Z0-9=, \-]```. |
| encrypted | Boolean | Indicates that the volume should be encrypted. |
| pool | Text | The name of the pool in which to place the volume. |

**Note:** `fsOwner`, `fsMode`, and `fsCreateOptions` are not applicable when using `volumeMode: Block` in the `PersistentVolumeClaim`.

### Pod inline volume parameters (Local Ephemeral Volumes)
These parameters are applicable only for Pod inline volumes and to be specified within Pod spec.

| Parameter | String | Description |
| --------- | ------ | ----------- |
| csi.storage.k8s.io/ephemeral | Boolean | Indicates that the request is for ephemeral inline volume. This is a mandatory parameter and must be set to "true".|
| inline-volume-secret-name | Text | A reference to the secret object containing sensitive information to pass to the CSI driver to complete the CSI NodePublishVolume call.|
| inline-volume-secret-namespace | Text | The namespace of `inline-volume-secret-name` for ephemeral inline volume.|
| size | Text | The size of ephemeral volume specified in MiB or GiB. If unspecified, a default value will be used.|

**Note:** The `NodePublishSecretRef` and (`inline-volume-secret-name`, `inline-volume-secret-namespace`) are mutually exclusive. One of them must be specified for ephemeral inline volume.

### Cloning parameters
Cloning supports two modes of cloning. Either use `cloneOf` and reference a PV of an existing PVC or use `importVolAsClone` and reference a Nimble volume name to clone and import to Kubernetes.

| Parameter | String | Description |
| --------- | ------ | ----------- |
| cloneOf | Text | The name of the PV to be cloned. `cloneOf` and `importVolAsClone` are mutually exclusive. |
| importVolAsClone | Text | The name of the Nimble volume to clone and import. `importVolAsClone` and `cloneOf` are mutually exclusive. |
| snapshot | Text | The name of the snapshot to base the clone on. This is optional. If not specified, a new snapshot is created. |
| createSnapshot | Boolean | Indicates that a new snapshot of the volume should be taken matching the name provided in the `snapshot` parameter. If the `snapshot` parameter is not specified, a default name will be created. |

### Import parameters
Importing volumes to Kubernetes requires the source Nimble volume to be offline. In case of reverse replication, the upstream volume should be in offline state. All previous Access Control Records and Initiator Groups will be stripped from the volume when put under control of the HPE CSI Driver.

| Parameter | String | Description |
| --------- | ------ | ----------- |
| importVolumeName | Text | The name of the Nimble volume to import. |
| snapshot | Text | The name of the Nimble snapshot to restore the imported volume to after takeover. If not specified, the volume will not be restored. |
| takeover | Boolean | Indicates the current group will takeover ownership of the Nimble volume and volume collection. This should be performed against a downstream replica. |
| reverseReplication | Boolean | Reverses the replication direction so that writes to the Nimble volume are replicated back to the group where it was replicated from. |
| forceImport | Boolean | Forces the import of a volume that is not owned by the group and is not part of a volume collection. If the volume is part of a volume collection, use takeover instead.

## HPE Nimble Storage VolumeSnapshotClass parameters for the HPE CSI Driver

| Parameter | String | Description |
| --------- | ------ | ----------- |
| description | Text | Text to be added to the snapshot's description on the Nimble array. |
| writable | Boolean | Indicates if the snapshot is writable on the Nimble array. |
| online | Boolean | Indicates if the snapshot is set to online on the Nimble array.|
