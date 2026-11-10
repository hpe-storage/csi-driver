#!/bin/sh

# Tolerate zero errors and tell the world all about it
set -xe

# Check if we are running as node-plugin
for arg in "$@"; do
    if [ "$arg" = "--node-service" ]; then
        nodeService=true
    fi
    if [ "$arg" = "--node-init" ]; then
        nodeInit=true
    fi
done

# Set up node configurations based on environment variables
disableNodeConformance=${DISABLE_NODE_CONFORMANCE}
disableNodeConfiguration=${DISABLE_NODE_CONFIGURATION}

if [ "$nodeService" = true ] || [ "$nodeInit" = true ]; then
    # Copy HPE Log Collector diag script
    echo "copying hpe log collector diag script"
    cp -f "/opt/hpe-storage/bin/hpe-logcollector.sh" \
        /usr/local/bin/hpe-logcollector.sh
    chmod +x /usr/local/bin/hpe-logcollector.sh

    # symlink to host iscsi/multipath config files
    ln -s /host/etc/multipath.conf /etc/multipath.conf
    ln -s /host/etc/multipath /etc/multipath
    ln -s /host/etc/iscsi /etc/iscsi
    ln -s /host/etc/nvme /etc/nvme

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

if [ "$nodeInit" = true ]; then
    # Disable the conformance checks
    if [ "$disableNodeConformance" = "true" ]; then
        echo "Node conformance checks are disabled"
        disableConformanceCheck=true
    fi

    # Completely disable the node configuration by the driver.
    if [ "$disableNodeConfiguration" = "true" ]; then
        echo "Node configuration is disabled"
        disableConformanceCheck=true
    fi

    if [ "$disableConformanceCheck" != true ]; then
        # Copy HPE Storage Node Conformance checks and conf in place.
        # ESC-18251: `systemctl daemon-reload` is a full reload of the host's
        # unit graph; on a densely loaded node it can exceed the systemctl
        # client's D-Bus timeout, which ended the init container with a
        # non-zero exit and left the node plugin in Init:CrashLoopBackOff.
        # Assert the current state before asking PID 1 for anything:
        #   - reload only when the unit file we install actually differs,
        #   - restart only when the unit is not already active.
        unitChanged=true
        if cmp -s "/opt/hpe-storage/lib/hpe-storage-node.service" \
            /etc/systemd/system/hpe-storage-node.service; then
            unitChanged=false
        else
            cp -f "/opt/hpe-storage/lib/hpe-storage-node.service" \
                /etc/systemd/system/hpe-storage-node.service
        fi
        cp -f "/opt/hpe-storage/lib/hpe-storage-node.sh" \
            /etc/hpe-storage/hpe-storage-node.sh
        chmod +x /etc/hpe-storage/hpe-storage-node.sh

        echo "running node conformance checks..."
        if [ "$unitChanged" = true ]; then
            systemctl daemon-reload
            systemctl restart hpe-storage-node
        elif systemctl is-active --quiet hpe-storage-node; then
            echo "hpe-storage-node is already active and unchanged; skipping daemon-reload and restart"
        else
            systemctl restart hpe-storage-node
        fi
    fi

    # Copy /etc/multipath.conf template if missing on host
    if [ ! -f /host/etc/multipath.conf ] &&
        [ "$disableNodeConfiguration" != true ]; then
        cp /opt/hpe-storage/nimbletune/multipath.conf.upstream /host/etc/multipath.conf
    fi

    # One shot NodeMonitor to ensure node is ready
    exec /bin/csi-driver $@
    exit $?
fi

echo "Starting CSI plugin..."

# Serve! Serve!!!
exec /bin/csi-driver $@
