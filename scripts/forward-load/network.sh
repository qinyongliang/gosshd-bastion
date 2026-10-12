#!/usr/bin/env bash
set -euo pipefail
if [ "$#" -lt 3 ]; then
 echo "usage: bash network.sh SERVER_BINARY AGENT_BINARY RESULTS_DIRECTORY [runner options]" >&2
 exit 2
fi
script_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
server="$1"; agent="$2"; results="$3"; shift 3
# A new outer namespace scopes every interface/route. Unique child namespace
# names and owned PIDs are cleaned by runner.py; no production processes touched.
exec sudo -n unshare --net --pid --fork --mount-proc python3 "$script_dir/runner.py" --server "$server" --agent "$agent" --results "$results" "$@"
