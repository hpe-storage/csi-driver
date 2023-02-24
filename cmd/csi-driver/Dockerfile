FROM registry.access.redhat.com/ubi9/ubi-minimal:9.1.0-1760.1675784957

RUN microdnf update -y && rm -rf /var/cache/yum
ADD AlmaLinux-Base.repo /etc/yum.repos.d/

RUN microdnf install -y cryptsetup

COPY --from=registry.access.redhat.com/ubi7/ubi:7.9-937 /usr/bin/systemctl /usr/bin/systemctl
COPY --from=registry.access.redhat.com/ubi7/ubi:7.9-937 /usr/lib64/libgcrypt.so.11.8.2 /usr/lib64/libgcrypt.so.11.8.2

RUN ln -s /usr/lib64/libgcrypt.so.11.8.2 /usr/lib64/libgcrypt.so.11


LABEL name="HPE CSI Driver for Kubernetes" \
      maintainer="HPE Storage" \
      vendor="HPE" \
      version="2.3.0-beta" \
      summary="HPE CSI Driver for Kubernetes" \
      description="The HPE CSI Driver for Kubernetes enables container orchestrators, such as Kubernetes and OpenShift, to manage the life-cycle of persistent storage." \
      io.k8s.display-name="HPE CSI Driver for Kubernetes" \
      io.k8s.description="The HPE CSI Driver for Kubernetes enables container orchestrators, such as Kubernetes and OpenShift, to manage the life-cycle of persistent storage." \
      io.openshift.tags=hpe,csi,hpe-csi-driver

WORKDIR /root
COPY /LICENSE /licenses/

RUN mkdir /chroot
ADD chroot-host-wrapper.sh /chroot
RUN chmod 777 /chroot/chroot-host-wrapper.sh
RUN ln -s /chroot/chroot-host-wrapper.sh /chroot/blkid \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/blockdev \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/iscsiadm \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/lsblk \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/lsscsi \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/mkfs.ext3 \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/mkfs.ext4 \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/mkfs.xfs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/mkfs.btrfs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/xfs_growfs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/resize2fs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/btrfs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/fsck \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/mount \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/multipath \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/multipathd \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/umount \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/ip \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/dmidecode \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/dnsdomainname \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/sg_inq \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/find

ENV PATH="/chroot:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# add host conformance checks and configuration
ADD [ "conform/*", "/opt/hpe-storage/lib/" ]

# diag log script
ADD [ "diag/*",  "/opt/hpe-storage/bin/" ]

# add config files to tune multipath settings
ADD [ "tune/*", "/opt/hpe-storage/nimbletune/"]

# add rescan script for resize operation
ADD ["rescan-scsi-bus.sh", "/usr/bin/rescan-scsi-bus.sh"]
RUN ["chmod", "+x", "/usr/bin/rescan-scsi-bus.sh"]

# add plugin binary
ADD [ "csi-driver", "/bin/" ]

# entrypoint
ADD [ "conform/entrypoint.sh", "/entrypoint.sh" ]
RUN [ "chmod", "+x", "/entrypoint.sh" ]

ENTRYPOINT [ "/entrypoint.sh" ]
