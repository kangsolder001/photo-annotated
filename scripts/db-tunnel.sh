#!/usr/bin/env bash
# Membuka SSH tunnel ke PostgreSQL shared di Raspi (hanya listen di 127.0.0.1:5432 pada Pi)
# agar test integrasi bisa jalan dari mesin dev: localhost:15432 -> Pi:5432.
# Pakai: scripts/db-tunnel.sh [user@host]   (default: myrpi@192.168.1.222)
# Autentikasi: kunci SSH, atau set SSHPASS dan pasang sshpass.
set -euo pipefail
target="${1:-myrpi@192.168.1.222}"
port=15432
if (exec 3<>/dev/tcp/127.0.0.1/$port) 2>/dev/null; then echo "port $port sudah terbuka"; exit 0; fi
ssh_cmd=(ssh -f -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -L "$port:127.0.0.1:5432" "$target")
if [ -n "${SSHPASS:-}" ]; then sshpass -e "${ssh_cmd[@]}"; else "${ssh_cmd[@]}"; fi
echo "tunnel siap di 127.0.0.1:$port"
