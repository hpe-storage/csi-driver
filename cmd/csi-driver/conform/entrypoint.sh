#!/bin/sh

# Tolerate zero errors and tell the world all about it
set -xe

# Check if we are running as node-plugin
for arg in "$@"
do
    if [ "$arg" = "--node-service" ]; then
        nodeservice=true
    fi
done

if [ "$nodeservice" = true ]; then
    if [ "${DISABLE_NODE_CONFORMANCE}" = "true" ]; then
        echo "node conformance checks are disabled"
    else
        # Copy HPE Storage Node Conformance checks and conf in place
        cp -f "/opt/hpe-storage/lib/hpe-storage-node.service" \
          /etc/systemd/system/hpe-storage-node.service
        cp -f "/opt/hpe-storage/lib/hpe-storage-node.sh" \
          /etc/hpe-storage/hpe-storage-node.sh
        chmod +x /etc/hpe-storage/hpe-storage-node.sh

        echo "running node conformance checks..."
        # Reload and run!
        systemctl daemon-reload
        systemctl restart hpe-storage-node
    fi

    # Copy HPE Log Collector diag script
    echo "copying hpe log collector diag script"
    cp -f "/opt/hpe-storage/bin/hpe-logcollector.sh" \
        /usr/local/bin/hpe-logcollector.sh
    chmod +x  /usr/local/bin/hpe-logcollector.sh

    # Copy /etc/multipath.conf template if missing on host
    if [ ! -f /host/etc/multipath.conf ]; then
        cp /opt/hpe-storage/nimbletune/multipath.conf.upstream /host/etc/multipath.conf
    fi
    # symlink to host iscsi/multipath config files
    ln -s /host/etc/multipath.conf /etc/multipath.conf
    ln -s /host/etc/multipath /etc/multipath
    ln -s /host/etc/iscsi /etc/iscsi
    # symlink to host os release files for parsing
    if [ -f /host/etc/redhat-release ]; then
        # remove existing file from ubi
        rm /etc/redhat-release
        ln -s /host/etc/redhat-release /etc/redhat-release
    fi
    if [ -f /host/etc/os-release ]; then
        # remove existing file from ubi
        rm /etc/os-release
        ln -s /host/etc/os-release /etc/os-release
    fi
fi

echo "starting csi plugin..."
# Serve! Serve!!!
exec /bin/csi-driver $@