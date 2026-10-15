# Path-checked agent process helpers.
# agent_target is a canonical absolute path, never a process-name kill pattern.
agent_pid_matches() {
  case "$1" in ''|*[!0-9]*|0|1) return 1 ;; esac
  if [ "$os" = darwin ]; then
    lsof -t -a -p "$1" -d txt "$agent_target" 2>/dev/null | grep -qx "$1"
  else
    agent_exe="$(readlink "/proc/$1/exe" 2>/dev/null || true)"
    case "$agent_exe" in "$agent_target"|"$agent_target (deleted)") return 0 ;; *) return 1 ;; esac
  fi
}
agent_matching_pids() {
  if [ "$os" = darwin ]; then
    # lsof returns 1 when no process is using the executable.
    lsof -t -a -d txt "$agent_target" 2>/dev/null || [ "$?" = 1 ]
  else
    for agent_exe_link in /proc/[0-9]*/exe; do
      agent_candidate="${agent_exe_link#/proc/}"
      agent_candidate="${agent_candidate%/exe}"
      if agent_pid_matches "$agent_candidate"; then printf '%s\n' "$agent_candidate"; fi
    done
  fi
}
stop_agent_processes() {
  if [ "$os" = darwin ]; then
    command -v lsof >/dev/null 2>&1 || { echo 'lsof is required to safely stop the installed agent.' >&2; return 1; }
  else
    command -v readlink >/dev/null 2>&1 || { echo 'readlink is required to safely stop the installed agent.' >&2; return 1; }
  fi
  agent_pids="$(agent_matching_pids)" || return 1
  for agent_pid in $agent_pids; do
    if agent_pid_matches "$agent_pid"; then
      echo "Stopping existing agent (pid=$agent_pid)..."
      if ! kill "$agent_pid"; then
        agent_pid_matches "$agent_pid" && return 1
      fi
      agent_attempt=0
      while agent_pid_matches "$agent_pid" && [ "$agent_attempt" -lt 10 ]; do
        sleep 1
        agent_attempt=$((agent_attempt + 1))
      done
      if agent_pid_matches "$agent_pid"; then
        if ! kill -KILL "$agent_pid"; then
          agent_pid_matches "$agent_pid" && return 1
        fi
        agent_attempt=0
        while agent_pid_matches "$agent_pid" && [ "$agent_attempt" -lt 5 ]; do
          sleep 1
          agent_attempt=$((agent_attempt + 1))
        done
      fi
      if agent_pid_matches "$agent_pid"; then
        echo "Timed out waiting for the old agent (pid=$agent_pid) to exit; installation aborted." >&2
        return 1
      fi
    fi
  done
  return 0
}
# End path-checked agent process helpers.
