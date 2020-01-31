#!/usr/bin/env bash

ME=`basename "$0"`
DIR="/host"
if [ ! -d "${DIR}" ]; then
    echo "Could not find docker engine host's filesystem at expected location: ${DIR}"
    exit 1
fi

exec chroot /host /usr/bin/env -i PATH="/sbin:/bin:/usr/bin:/usr/sbin" ${ME} "${@:1}"
