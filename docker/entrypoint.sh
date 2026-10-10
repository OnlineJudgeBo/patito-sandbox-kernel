#!/bin/bash
# Prepares control groups for isolate and starts the judge.
# The container must run privileged: isolate creates namespaces and one cgroup per box.
set -euo pipefail

CGROUP=/sys/fs/cgroup
if [ ! -f "$CGROUP/cgroup.controllers" ]; then
    echo "patito-judge needs cgroup v2 mounted at $CGROUP" >&2
    exit 1
fi

# cgroup v2 only delegates controllers from a group without processes of its own:
# move everything out of the root group first.
mkdir -p "$CGROUP/init"
for pid in $(cat "$CGROUP/cgroup.procs"); do
    echo "$pid" > "$CGROUP/init/cgroup.procs" 2>/dev/null || true
done
echo "+memory +pids +cpuset" > "$CGROUP/cgroup.subtree_control"
mkdir -p "$CGROUP/isolate"
echo "+memory +pids +cpuset" > "$CGROUP/isolate/cgroup.subtree_control"
mkdir -p /run/isolate/locks

exec /usr/local/bin/patito-judge "$@"
