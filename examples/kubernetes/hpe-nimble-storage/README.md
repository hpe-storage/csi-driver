# HPE Nimble Storage StorageClass parameters for the HPE CSI Driver
A `StorageClass` is used to provision or clone an HPE Nimble Storage-backed persistent volume.  It can also be used to import an existing HPE Nimble Storage volume or clone of a snapshot into the Kubernetes cluster. The parameters are grouped below by those same workflows.

# Provisioning parameters
| Parameter | String | Description |
| --------- | ------ | ----------- |
|fsOwner | userId:groupId | The user id and group id that should own the root directory of the filesystem |
|fsMode | Octal digits | 1 to 4 octal digits that represent the file mode to be applied to the root directory of the filesystem |
| limitIops | Integer | The IOPS limit of the volume. The IOPS limit should be in the range 256 to 4294967294, or -1 for unlimited (default) |.

* limitMbps
  * The MB/s throughput limit for the volume.
* destroyOnDelete
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
  * "true" or "false". Indicates the backing volume is tied to the lifecycle of the pod.  The volume will be created when the pod is created and the volume will be deleted when the pod is deleted.
* destroyOnDelete
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
  * "true" or "false". Indicates the backing volume is tied to the lifecycle of the pod.  The volume will be created when the pod is created and the volume will be deleted when the pod is deleted.
* destroyOnDelete
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
