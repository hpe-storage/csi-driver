# throw away builder image
FROM --platform=$BUILDPLATFORM registry.access.redhat.com/ubi9/ubi:9.3-1552 AS build

# install prereqs
RUN dnf install -y make wget golang

# build driver
WORKDIR /usr/src/hpe-csi-driver
ADD cmd ./cmd
ADD vendor ./vendor
ADD pkg ./pkg
ADD Makefile .
ADD go.mod .
ADD go.sum .
ARG TARGETARCH
ARG TARGETPLATFORM
RUN make ARCH=$TARGETARCH compile # $TARGETPLATFORM

# image build
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.3-1552

RUN microdnf update -y && rm -rf /var/cache/yum
ADD cmd/csi-driver/AlmaLinux-Base.repo /etc/yum.repos.d/

RUN microdnf install -y cryptsetup tar procps

COPY --from=centos:centos7.9.2009 /usr/bin/systemctl /usr/bin/systemctl
COPY --from=centos:7.9.2009 /usr/lib64/libgcrypt.so.11.8.2 /usr/lib64/libgcrypt.so.11.8.2

RUN ln -s /usr/lib64/libgcrypt.so.11.8.2 /usr/lib64/libgcrypt.so.11

LABEL name="HPE CSI Driver for Kubernetes" \
      maintainer="HPE Storage" \
      vendor="HPE" \
      version="2.4.1-beta2" \
      summary="HPE CSI Driver for Kubernetes" \
      description="The HPE CSI Driver for Kubernetes enables container orchestrators, such as Kubernetes and OpenShift, to manage the life-cycle of persistent storage." \
      io.k8s.display-name="HPE CSI Driver for Kubernetes" \
      io.k8s.description="The HPE CSI Driver for Kubernetes enables container orchestrators, such as Kubernetes and OpenShift, to manage the life-cycle of persistent storage." \
      io.openshift.tags=hpe,csi,hpe-csi-driver

WORKDIR /root
COPY LICENSE /licenses/

RUN mkdir /chroot
ADD cmd/csi-driver/chroot-host-wrapper.sh /chroot
RUN chmod 777 /chroot/chroot-host-wrapper.sh
RUN ln -s /chroot/chroot-host-wrapper.sh /chroot/blkid \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/rescan-scsi-bus.sh \
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
ADD [ "cmd/csi-driver/conform/*", "/opt/hpe-storage/lib/" ]

# diag log script
ADD [ "cmd/csi-driver/diag/*",  "/opt/hpe-storage/bin/" ]

# add config files to tune multipath settings
ADD [ "vendor/github.com/hpe-storage/common-host-libs/tunelinux/config/*", "/opt/hpe-storage/nimbletune/"]

# add plugin binary
COPY --from=build /usr/src/hpe-csi-driver/build/csi-driver /bin/

# entrypoint
ADD [ "cmd/csi-driver/conform/entrypoint.sh", "/entrypoint.sh" ]
RUN [ "chmod", "+x", "/entrypoint.sh" ]

ENTRYPOINT [ "/entrypoint.sh" ]
