# throw away builder image
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi:9.8-1791888712 AS build

# install prereqs
RUN dnf install -y make golang

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
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8-1791898448

RUN microdnf update -y && rm -rf /var/cache/yum
ADD cmd/csi-driver/AlmaLinux-Base.repo /etc/yum.repos.d/

# systemd provides the systemctl client used by node-init against the host's PID 1.
# ESC-18251: the CentOS 7 systemd-219 systemctl previously copied over this file
# gives up after a fixed 25 s D-Bus timeout, which a densely loaded node cannot meet
# for `daemon-reload`. The UBI 9 systemd-252 client waits up to 180 s for a reload
# and honours SYSTEMD_BUS_TIMEOUT for all other method calls.
#
# diffutils provides `cmp`, which the entrypoint uses to decide whether the
# conformance unit changed; without it every start requested a daemon-reload
# (ESC-18251 fix verification F1). The build asserts both are present.
#
# systemd and diffutils are installed with the third-party AlmaLinux repo disabled
# so that they come from the Red Hat UBI BaseOS repo (fix verification F6: the
# first fix image shipped systemd-252-67.el9_8.6.alma.1 in a RHEL 9.8 base, which
# is a certification/provenance concern). The Alma repo stays available for the
# packages that need it.
RUN microdnf install -y --disablerepo=BaseOS cryptsetup tar procps systemd diffutils \
    && rm -rf /var/cache/yum \
    && command -v cmp \
    && rpm -q --qf 'systemd %{VERSION}-%{RELEASE} vendor=%{VENDOR}\n' systemd \
    && test -z "$(rpm -q --qf '%{RELEASE}' systemd | grep -i alma)"

# Allow slow (busy PID 1) systemd method calls to complete instead of timing out.
ENV SYSTEMD_BUS_TIMEOUT=180
# ESC-18251 fix verification F3: with hostPID=true, systemctl sees /proc/1/root != /
# and would print "Running in chroot, ignoring command" and exit 0 without acting.
# The entrypoint also sets this; it is here so that ad-hoc `systemctl` runs in the
# image (probes, log collectors) behave the same way.
ENV SYSTEMD_IGNORE_CHROOT=1

LABEL name="HPE CSI Driver for Kubernetes" \
    maintainer="HPE Storage" \
    vendor="HPE" \
    version="3.4.0-alpha1" \
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
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/dumpe2fs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/xfs_io \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/xfs_growfs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/xfs_repair \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/tune2fs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/resize2fs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/btrfs \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/fsck \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/mount \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/multipath \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/multipathd \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/umount \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/ip \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/dnsdomainname \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/sg_inq \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/find \
    && ln -s /chroot/chroot-host-wrapper.sh /chroot/nvme

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
