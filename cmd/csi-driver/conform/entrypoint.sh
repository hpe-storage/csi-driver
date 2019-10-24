#!/bin/sh

# Tolerate zero errors and tell the world all about it
set -xe

# Check if we are running as node-plugin
for arg in "$@"
do
    if [ "$arg" = "--node-service" ]; then
        if [ ! -z "$iSCSI_INITIATOR_NAME" ]; then
            echo "InitiatorName=${iSCSI_INITIATOR_NAME}" > /etc/iscsi/initiatorname.iscsi
        fi
        iscsid -f &
        multipathd -d &
    fi
done

echo "starting csi plugin..."
# Serve! Serve!!!
exec /bin/csi-driver $@
