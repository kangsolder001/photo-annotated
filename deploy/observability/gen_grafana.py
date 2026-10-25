#!/usr/bin/env python3
# Membuat grafana/dashboards/photo-annotated.json dan grafana/provisioning/alerting/rules.yaml.
# Pakai: python3 gen_grafana.py .   (butuh PyYAML)
import json, sys
out = sys.argv[1]
APP = '{project="photo-annotated", service="api"}'
REQ = APP + ' |= `"msg":"request"`'
STATS = APP + ' |= `"msg":"stats"` | json'
L = {"type": "loki", "uid": "loki"}
P = {"type": "prometheus", "uid": "prometheus"}

panels, y = [], 0
def row(title):
    global y
    panels.append({"type": "row", "title": title, "collapsed": False, "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "id": len(panels)+1, "panels": []})
    y += 1
def panel(title, ds, targets, kind="timeseries", unit=None, w=8, h=8, x=0, thresholds=None, desc=None, maxv=None):
    p = {"id": len(panels)+1, "type": kind, "title": title, "datasource": ds,
         "gridPos": {"h": h, "w": w, "x": x, "y": y},
         "targets": [dict(refId=chr(65+i), datasource=ds, expr=e, legendFormat=lf, **({"queryType": "range"} if ds is L else {})) for i, (e, lf) in enumerate(targets)],
         "fieldConfig": {"defaults": {}, "overrides": []}, "options": {}}
    if desc: p["description"] = desc
    d = p["fieldConfig"]["defaults"]
    if unit: d["unit"] = unit
    if maxv is not None: d["max"] = maxv
    if thresholds:
        d["thresholds"] = {"mode": "absolute", "steps": [{"color": "green", "value": None}] + [{"color": c, "value": v} for v, c in thresholds]}
        if kind == "timeseries": d["custom"] = {"thresholdsStyle": {"mode": "line"}}
    panels.append(p)

row("Photo Annotated — API")
panel("Request per menit", L, [(f'sum by (route) (count_over_time({REQ} | json [1m]))', "{{route}}")], w=8)
panel("Error rate 5xx (%)", L, [(f'100 * sum(count_over_time({REQ} | json | status >= 500 [5m])) / sum(count_over_time({REQ} [5m]))', "5xx %")], unit="percent", thresholds=[(2, "red")], w=8, x=8)
panel("Latency p95 per route", L, [(f'quantile_over_time(0.95, {REQ} | json | unwrap duration_ms [5m]) by (route)', "{{route}}")], unit="ms", w=8, x=16)
y += 8
panel("Kode error (4xx/5xx)", L, [(f'sum by (code) (count_over_time({APP} |= `"code":` | json | code != "" [5m]))', "{{code}}")], w=8,
      desc="Termasuk UPLOAD_BUSY, STORAGE_FULL, STORAGE_UNAVAILABLE, RATE_LIMITED, OTP_*")
panel("Upload aktif vs batas", L, [(f'max(max_over_time({STATS} | unwrap uploads_active [2m]))', "aktif"), (f'max(max_over_time({STATS} | unwrap uploads_max [2m]))', "batas")], w=8, x=8)
panel("Foto pending (belum dipakai submission)", L, [(f'max(max_over_time({STATS} | unwrap photos_pending [2m]))', "pending")], w=8, x=16)
y += 8
row("Raspberry Pi")
panel("Suhu CPU", P, [('max(node_thermal_zone_temp)', "°C")], unit="celsius", thresholds=[(70, "orange"), (75, "red")], w=6)
panel("CPU (%)", P, [('100 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100', "CPU")], unit="percent", maxv=100, w=6, x=6)
panel("Memori terpakai", P, [('node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes', "terpakai"), ('node_memory_MemTotal_bytes', "total")], unit="bytes", w=6, x=12)
panel("SSD terpakai (%)", P, [('100 * (1 - node_filesystem_avail_bytes{mountpoint="/mnt/ssd"} / node_filesystem_size_bytes{mountpoint="/mnt/ssd"})', "/mnt/ssd"), ('100 * (1 - node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"})', "microSD /")], unit="percent", maxv=100, thresholds=[(80, "red")], w=6, x=18)
y += 8
panel("Trafik eth0", P, [('rate(node_network_receive_bytes_total{device="eth0"}[5m]) * 8', "masuk"), ('rate(node_network_transmit_bytes_total{device="eth0"}[5m]) * 8', "keluar")], unit="bps", w=12)
panel("I/O SSD", P, [('rate(node_disk_written_bytes_total{device="sda"}[5m])', "tulis"), ('rate(node_disk_read_bytes_total{device="sda"}[5m])', "baca"), ('rate(node_disk_io_time_seconds_total{device="sda"}[5m]) * 100', "sibuk %")], unit="Bps", w=12, x=12)
y += 8
row("OTP WhatsApp")
panel("Hasil kirim OTP", L, [(f'sum by (outcome) (count_over_time({APP} |= `"msg":"otp dispatch` | json [5m]))', "{{outcome}}")], w=8)
panel("Antrean OTP", L, [(f'max(max_over_time({STATS} | unwrap otp_queued [2m]))', "antre"), (f'max(max_over_time({STATS} | unwrap otp_max_queue [2m]))', "max_queue")], w=8, x=8)
panel("OTP 24 jam vs batas harian", L, [(f'max(max_over_time({STATS} | unwrap otp_created_24h [2m]))', "dibuat"), (f'max(max_over_time({STATS} | unwrap otp_sent_24h [2m]))', "terkirim"), (f'max(max_over_time({STATS} | unwrap otp_failed_24h [2m]))', "gagal"), (f'max(max_over_time({STATS} | unwrap otp_daily_cap [2m]))', "batas")], w=8, x=16)
y += 8
panel("Log error terbaru (semua aplikasi)", L, [('{project=~".+"} |~ `"level":"(ERROR|error)"|level=error`', "")], kind="logs", w=24, h=10)

dash = {"uid": "photo-annotated", "title": "Photo Annotated & Raspberry Pi", "tags": ["photo-annotated", "raspi"],
        "timezone": "Asia/Jakarta", "refresh": "1m", "schemaVersion": 39, "time": {"from": "now-6h", "to": "now"},
        "panels": panels, "editable": False}
json.dump(dash, open(f"{out}/grafana/dashboards/photo-annotated.json", "w"), indent=2, ensure_ascii=False)

# ---------------------------------------------------------------- alert rules
def q(ref, ds, expr, instant=True):
    model = {"refId": ref, "expr": expr, "datasource": ds}
    if ds is L: model["queryType"] = "instant"
    else: model["instant"] = True
    return {"refId": ref, "relativeTimeRange": {"from": 600, "to": 0}, "datasourceUid": ds["uid"], "model": model}
def rule(uid, title, ds, expr, op, thr, for_="5m", summary="", nodata="OK"):
    return {"uid": uid, "title": title, "condition": "C", "for": for_, "noDataState": nodata, "execErrState": "Error",
            "labels": {"app": "photo-annotated"}, "annotations": {"summary": summary},
            "data": [q("A", ds, expr),
                     {"refId": "B", "datasourceUid": "__expr__", "model": {"refId": "B", "type": "reduce", "expression": "A", "reducer": "last", "settings": {"mode": "dropNN"}}},
                     {"refId": "C", "datasourceUid": "__expr__", "model": {"refId": "C", "type": "threshold", "expression": "B", "conditions": [{"evaluator": {"type": op, "params": [thr]}}]}}]}

rules = [
  rule("pa-error-rate", "Error rate API > 2%", L, f'100 * sum(count_over_time({REQ} | json | status >= 500 [5m])) / sum(count_over_time({REQ} [5m]))', "gt", 2, summary="Lebih dari 2% request API berakhir 5xx selama 5 menit"),
  rule("pa-api-silent", "API tidak mengirim stats (mati / SSD tidak ter-mount)", L, f'sum(count_over_time({APP} |= `"msg":"stats"` [5m]))', "lt", 1, for_="1m", nodata="Alerting", summary="Tidak ada baris stats 5 menit: container API mati atau gagal start (mis. STORAGE_UNAVAILABLE)"),
  rule("pa-storage-unavailable", "Penyimpanan foto tidak tersedia", L, f'sum(count_over_time({APP} |= `STORAGE_UNAVAILABLE` [5m]))', "gt", 0, for_="0s", summary="API melaporkan STORAGE_UNAVAILABLE (SSD/penanda hilang)"),
  rule("pa-cleanup-failing", "Job cleanup gagal berulang", L, f'sum(count_over_time({APP} |= `"msg":"job error"` |= `"job":"cleanup"` [3h]))', "gt", 1, for_="0s", summary="Cleanup gagal lebih dari sekali dalam 3 jam"),
  rule("pi-cpu-temp", "Suhu CPU Pi > 75°C", P, 'max(node_thermal_zone_temp)', "gt", 75, summary="Risiko thermal throttling"),
  rule("pi-ssd-usage", "SSD terpakai > 80%", P, '100 * (1 - node_filesystem_avail_bytes{mountpoint="/mnt/ssd"} / node_filesystem_size_bytes{mountpoint="/mnt/ssd"})', "gt", 80, summary="Ruang SSD hampir habis"),
  rule("pi-ssd-missing", "SSD tidak ter-mount", P, 'count(node_filesystem_size_bytes{mountpoint="/mnt/ssd"})', "lt", 1, for_="2m", nodata="Alerting", summary="/mnt/ssd hilang dari daftar filesystem"),
  rule("pi-kernel-io", "Error I/O disk di log kernel", L, 'sum(count_over_time({job="kernel"} |~ `(?i)(I/O error|EXT4-fs error|blk_update_request|reset (high|super)-speed USB|uas_eh)` [10m]))', "gt", 0, for_="0s", summary="Kernel melaporkan error disk/USB (cek kabel & SSD)"),
  rule("pi-eth0-saturated", "Trafik masuk eth0 > 80% kapasitas ISP", P, 'rate(node_network_receive_bytes_total{device="eth0"}[5m]) * 8', "gt", 150e6, summary="> 150 Mbps (80% dari ±187 Mbps) selama 5 menit"),
  rule("otp-channel-down", "Kanal WhatsApp OTP tidak tersedia", L, f'sum(count_over_time({STATS} | otp_available="false" [5m]))', "gt", 0, summary="wa-notif terputus atau gagal berulang (cek sesi WhatsApp, mungkin perlu restart/scan QR)"),
  rule("otp-gateway-failures", "Kegagalan wa-notif > 20%", L, f'100 * sum(count_over_time({APP} |= `"msg":"otp dispatch` |= `WA_UNAVAILABLE` [5m])) / sum(count_over_time({APP} |= `"msg":"otp dispatch` [5m]))', "gt", 20, summary="Lebih dari 20% pengiriman OTP gagal karena gateway"),
  rule("otp-queue-high", "Antrean OTP > 80% max_queue", L, f'max(max_over_time({STATS} | unwrap otp_queued [5m])) / max(max_over_time({STATS} | unwrap otp_max_queue [5m]))', "gt", 0.8, for_="2m", summary="Antrean OTP hampir penuh (register/login akan ditolak OTP_BUSY)"),
  rule("otp-daily-cap", "OTP harian > 80% daily_global_cap", L, f'max(max_over_time({STATS} | unwrap otp_created_24h [5m])) / max(max_over_time({STATS} | unwrap otp_daily_cap [5m]))', "gt", 0.8, for_="0s", summary="Mendekati batas harian OTP (kemungkinan penyalahgunaan)"),
]
import yaml
doc = {"apiVersion": 1, "groups": [{"orgId": 1, "name": "photo-annotated", "folder": "Alert", "interval": "1m", "rules": rules}]}
open(f"{out}/grafana/provisioning/alerting/rules.yaml", "w").write("# Dihasilkan dari daftar alert spec §12. Kontak notifikasi belum diatur (alert tampil di Grafana).\n" + yaml.safe_dump(doc, sort_keys=False, allow_unicode=True, width=1000))
