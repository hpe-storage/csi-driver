FROM debian:stretch
RUN apt-get update && \
    apt-get install -y \
    multipath-tools \
    e2fsprogs \
    btrfs-progs \
    xfsprogs \
    iproute2 \
    util-linux \
    systemd \
    libisns0 \
    gawk \
    nfs-common \
    dmidecode

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