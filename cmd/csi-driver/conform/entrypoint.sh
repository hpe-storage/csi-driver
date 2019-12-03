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
          /lib/systemd/system/hpe-storage-node.service
        cp -f "/opt/hpe-storage/lib/hpe-storage-node.sh" \
          /usr_local/local/bin/hpe-storage-node.sh
        chmod +x /usr_local/local/bin/hpe-storage-node.sh

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
fi

echo "starting csi plugin..."
# Serve! Serve!!!
exec /bin/csi-driver $@