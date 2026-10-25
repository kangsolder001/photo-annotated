#!/usr/bin/env bash
# Menyiapkan folder data di SSD (pemilik sesuai UID tiap image) dan .env Grafana. Aman diulang.
set -euo pipefail
root=/mnt/ssd/observability
mountpoint -q /mnt/ssd || { echo "SSD belum ter-mount di /mnt/ssd; batal (data tidak boleh ke microSD)"; exit 1; }
sudo mkdir -p "$root"/{loki,prometheus,alloy,grafana}
sudo chown 10001:10001 "$root/loki"        # loki
sudo chown 65534:65534 "$root/prometheus"  # nobody (prometheus)
sudo chown 472:0 "$root/grafana"           # grafana
cd "$(dirname "$0")"
if [ ! -f .env ]; then
  umask 077
  echo "GF_SECURITY_ADMIN_PASSWORD=$(head -c 24 /dev/urandom | base64 | tr -d '/+=')" > .env
  echo ".env dibuat (password admin Grafana di dalamnya)"
fi
