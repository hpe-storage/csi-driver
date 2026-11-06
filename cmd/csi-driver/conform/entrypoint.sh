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
    # ESC-18251 (fix verification F2/F3): systemd >= 252's systemctl refuses the
    # host's /run/systemd/private from the pod's own PID namespace ("Failed to
    # connect to bus: No data available") and, once the DaemonSet runs with
    # hostPID=true, treats /proc/1/root != / as a chroot and returns 0 WITHOUT
    # acting ("Running in chroot, ignoring command"). The node DaemonSet sets
    # hostPID=true; this variable makes the client actually act. Both are needed.
    export SYSTEMD_IGNORE_CHROOT="${SYSTEMD_IGNORE_CHROOT:-1}"

    # files_same A B -> 0 when A and B are byte-identical. Fails CLOSED: if no
    # comparison tool is available we say so and report "changed" explicitly
    # instead of letting a "command not found" silently force a daemon-reload on
    # every start (fix verification F1: cmp was not in the image).
    files_same() {
        if command -v cmp >/dev/null 2>&1; then
            cmp -s "$1" "$2"
        elif command -v sha256sum >/dev/null 2>&1; then
            [ -f "$2" ] && [ "$(sha256sum < "$1")" = "$(sha256sum < "$2")" ]
        else
            echo "WARNING: neither cmp nor sha256sum is available; treating $2 as changed"
            return 1
        fi
    }

    # note_skipped MESSAGE: leave a record in the node log so that "host services
    # unmanaged by the driver" is visible without the init container's log.
    note_skipped() {
        echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) node-init: $*" >> /var/log/hpe-csi-node.log 2>/dev/null || true
    }

    # run_systemctl VERB... : best effort, never ends the init, and names the real
    # failure (fix verification F4/N3): a connection refusal is not "PID 1 busy".
    run_systemctl() {
        rc=0
        out=$(systemctl "$@" 2>&1) || rc=$?
        if [ "$rc" -ne 0 ]; then
            case "$out" in
                *"Failed to connect to bus"*|*"No data available"*)
                    msg="systemctl $* SKIPPED: cannot connect to the host's systemd (the node DaemonSet needs hostPID=true)" ;;
                *"timed out"*|*"Connection timed out"*)
                    msg="systemctl $* SKIPPED: timed out waiting for the host's systemd (PID 1 busy; it will still complete the request)" ;;
                *)
                    msg="systemctl $* FAILED; continuing" ;;
            esac
            echo "WARNING: $msg. rc=$rc: $out"
            note_skipped "$msg (rc=$rc: $out)"
            return 0
        fi
        case "$out" in
            *"Running in chroot, ignoring"*)
                echo "WARNING: systemctl $* was IGNORED by systemctl (chroot heuristic); set SYSTEMD_IGNORE_CHROOT=1 in the init container. $out"
                note_skipped "systemctl $* ignored by systemctl (chroot heuristic)" ;;
            "") ;;
            *) echo "$out" ;;
        esac
        return 0
    }

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
        # ESC-18251: only ask PID 1 for a daemon-reload when the unit file
        # actually changed. On a densely loaded node a reload can take a
        # minute or more, and an unconditional reload on every restart of the
        # node plugin kept the init container in CrashLoopBackOff.
        unitChanged=false
        if ! files_same "/opt/hpe-storage/lib/hpe-storage-node.service" \
            /etc/systemd/system/hpe-storage-node.service; then
            cp -f "/opt/hpe-storage/lib/hpe-storage-node.service" \
                /etc/systemd/system/hpe-storage-node.service
            unitChanged=true
        fi
        # A script change needs no reload; the unit execs the script afresh.
        if ! files_same "/opt/hpe-storage/lib/hpe-storage-node.sh" \
            /etc/hpe-storage/hpe-storage-node.sh; then
            cp -f "/opt/hpe-storage/lib/hpe-storage-node.sh" \
                /etc/hpe-storage/hpe-storage-node.sh
        fi
        chmod +x /etc/hpe-storage/hpe-storage-node.sh

        echo "running node conformance checks..."
        # Reload (only if needed) and run. A systemd that is slow to answer must
        # not be fatal: PID 1 completes the request even after the client gives up,
        # and the node plugin can still serve volumes without the conformance unit.
        if [ "$unitChanged" = true ]; then
            run_systemctl daemon-reload
        fi
        run_systemctl restart hpe-storage-node
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
