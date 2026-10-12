# Forward validation lab

Run \network.sh\ through \run-wsl.sh\ for actual isolated server/Agent processes. See the exact commands and results in ../../docs/http-forward-load-results.md.

Prerequisites: Linux iproute2, Python3, sudo non-interactive permission for scoped namespaces, GCC for race, matching Go SDK. setup-wsl-go.sh verifies the official Go archive checksum and installs in a user-owned cache, not system-wide. A slow WSL download can be staged via the host and checksum-verified before extraction.

The runner asserts an otherwise-empty outer namespace, owns unique child namespace/interface names and its process groups, and retains temporary DBs/results for forensic checks. Never use these scripts with production URLs/Agents. Generated results, __pycache__, binaries and SDK archives are excluded from commits.
