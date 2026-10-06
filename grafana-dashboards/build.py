#!/usr/bin/env python3
"""Генератор дашбордов tsmonitor для Grafana (monitoring.otcnet.ru).

    python3 grafana-dashboards/build.py           # записать JSON в grafana-dashboards/
    python3 grafana-dashboards/build.py --push    # и отправить в Grafana (токен ~/.config/grafana-otcnet.token)

UID дашбордов прежние: ts-stream-overview (мозаика) и ts-stream-details (поток);
плитка мозаики ведёт на ts-stream-details?var-stream=…
"""
import json
import os
import sys
import urllib.request

GRAFANA = "https://monitoring.otcnet.ru"
TOKEN_FILE = os.path.expanduser("~/.config/grafana-otcnet.token")
HERE = os.path.dirname(os.path.abspath(__file__))

PROM = {"type": "prometheus", "uid": "${DS_PROMETHEUS}"}
LOKI = {"type": "loki", "uid": "P8E80F9AEF21F6940"}
JOB = 'job="tsmonitor"'
EVENTS = '{job="tsmonitor-events"'

DETAILS_URL = "/d/ts-stream-details?var-stream={stream}&var-description={desc}&${{__url_time_range}}"

_ids = iter(range(1, 1000))


def prom(expr, legend="", ref="A", instant=False, fmt=None, interval=None):
    t = {"datasource": PROM, "editorMode": "code", "expr": expr, "refId": ref,
         "legendFormat": legend or "__auto", "range": not instant, "instant": instant}
    if fmt:
        t["format"] = fmt
    if interval:
        t["interval"] = interval
    return t


def loki(expr, ref="A"):
    return {"datasource": LOKI, "editorMode": "code", "expr": expr, "queryType": "range", "refId": ref}


def panel(ptype, title, x, y, w, h, targets, unit=None, desc=None, **kw):
    p = {"id": next(_ids), "type": ptype, "title": title,
         "gridPos": {"x": x, "y": y, "w": w, "h": h},
         "datasource": targets[0]["datasource"] if targets else PROM,
         "targets": targets,
         "fieldConfig": {"defaults": {}, "overrides": []}, "options": {}}
    if unit:
        p["fieldConfig"]["defaults"]["unit"] = unit
    if desc:
        p["description"] = desc
    for k, v in kw.items():
        p[k] = v
    return p


def row(title, y, collapsed=False):
    return {"id": next(_ids), "type": "row", "title": title, "collapsed": collapsed,
            "gridPos": {"x": 0, "y": y, "w": 24, "h": 1}, "panels": []}


def thresholds(*steps):
    """thresholds(("green", None), ("orange", 1), ("red", 10))"""
    return {"mode": "absolute", "steps": [{"color": c, "value": v} for c, v in steps]}


def stat(title, x, y, w, h, targets, unit=None, steps=None, mappings=None, desc=None, text_mode="value",
         decimals=None, color_mode="background", calc="lastNotNull"):
    p = panel("stat", title, x, y, w, h, targets, unit, desc)
    d = p["fieldConfig"]["defaults"]
    d["color"] = {"mode": "thresholds"}
    d["thresholds"] = thresholds(*(steps or [("green", None)]))
    if mappings:
        d["mappings"] = mappings
    if decimals is not None:
        d["decimals"] = decimals
    p["options"] = {"colorMode": color_mode, "graphMode": "none", "justifyMode": "center",
                    "orientation": "auto", "textMode": text_mode, "wideLayout": True,
                    "reduceOptions": {"calcs": [calc], "fields": "", "values": False},
                    "showPercentChange": False}
    return p


def timeseries(title, x, y, w, h, targets, unit=None, desc=None, bars=False, stack=False, min0=True,
               steps=None, legend_calcs=("lastNotNull", "max")):
    p = panel("timeseries", title, x, y, w, h, targets, unit, desc)
    custom = {"drawStyle": "bars" if bars else "line", "lineWidth": 1, "fillOpacity": 60 if bars else 10,
              "showPoints": "never", "spanNulls": False, "axisSoftMin": 0 if min0 else None,
              "stacking": {"mode": "normal" if stack else "none", "group": "A"}}
    if steps:
        custom["thresholdsStyle"] = {"mode": "line+area"}
        p["fieldConfig"]["defaults"]["thresholds"] = thresholds(*steps)
    p["fieldConfig"]["defaults"]["custom"] = custom
    p["options"] = {"legend": {"displayMode": "table", "placement": "bottom", "showLegend": True,
                               "calcs": list(legend_calcs)},
                    "tooltip": {"mode": "multi", "sort": "desc"}}
    return p


def table(title, x, y, w, h, targets, rename=None, hide=(), desc=None, overrides=None, sort=None,
          transformations=None):
    p = panel("table", title, x, y, w, h, targets, desc=desc)
    p["fieldConfig"]["defaults"]["custom"] = {"align": "auto", "cellOptions": {"type": "auto"}, "inspect": False}
    p["fieldConfig"]["overrides"] = overrides or []
    tr = list(transformations or [])
    tr.append({"id": "organize", "options": {
        "excludeByName": {k: True for k in ("Time", "__name__", "instance", "job", *hide)},
        "renameByName": rename or {}}})
    p["transformations"] = tr
    p["options"] = {"showHeader": True, "cellHeight": "sm", "footer": {"show": False}}
    if sort:
        p["options"]["sortBy"] = [{"displayName": sort[0], "desc": sort[1]}]
    return p


def logs(title, x, y, w, h, expr, desc=None):
    p = panel("logs", title, x, y, w, h, [loki(expr)], desc=desc)
    p["options"] = {"showTime": True, "wrapLogMessage": True, "sortOrder": "Descending",
                    "enableLogDetails": True, "dedupStrategy": "none", "prettifyLogMessage": False,
                    "showLabels": False, "showCommonLabels": False}
    return p


def override(name, *props):
    return {"matcher": {"id": "byName", "options": name},
            "properties": [{"id": k, "value": v} for k, v in props]}


def stream_link_override(field="stream", desc_field="description"):
    url = DETAILS_URL.format(stream="${__data.fields.%s}" % field, desc="${__data.fields.%s}" % desc_field)
    return override(field, ("links", [{"title": "Открыть поток", "url": url}]))


DS_VAR = {"name": "DS_PROMETHEUS", "label": "Data Source", "type": "datasource", "query": "prometheus",
          "current": {"text": "Prometheus", "value": "PBFA97CFB590B2093"}, "refresh": 1, "includeAll": False}

STATUS_MAPPINGS = [{"type": "value", "options": {
    "0": {"text": "OFFLINE", "color": "red", "index": 0},
    "1": {"text": "ONLINE", "color": "green", "index": 1},
    "2": {"text": "CC ERR", "color": "orange", "index": 2}}}]

# Сводка одного события для журнала: сложные (CC, смена PID/сервиса) — строкой JSON целиком
EVENT_LINE = (
    '{{ if .message }}{{ .message }}{{ else if or (eq .kind "cc_errors") (eq .kind "pids_changed") (eq .kind "service_changed") }}'
    '{{ __line__ }}'
    '{{ else }}{{ .kind }}{{ if .reason }} ({{ .reason }}){{ end }}'
    '{{ if .down_duration_s }} — не было {{ .down_duration_s }} с{{ end }}'
    '{{ if .error }}: {{ .error }}{{ end }}{{ if .exits }}, перезапусков {{ .exits }}{{ end }}{{ end }}'
)
SCTE_LINE = (
    '{{ if .message }}{{ .message }}{{ else if eq .type "event" }}{{ .event_type }} {{ .progress }} id={{ .event_id }} #{{ .count }}'
    '{{ if .time_to_event_ms }} — через {{ .time_to_event_ms }} мс{{ end }}'
    '{{ if .pre_roll_ms }} — pre-roll {{ .pre_roll_ms }} мс{{ end }}'
    '{{ else }}{{ .command }} id={{ .event_id }} out_of_network={{ .out_of_network }}'
    '{{ if .break_duration_s }} блок {{ .break_duration_s }} с{{ end }}{{ end }}'
)


def details():
    s = 'stream="$stream"'
    P = []
    # --- Плитки ---
    P.append(stat("Статус", 0, 0, 3, 4, [prom(f"ts_stream_status{{{s}}}")],
                  steps=[("red", None), ("green", 1)], mappings=STATUS_MAPPINGS))
    P.append(stat("Битрейт TS", 3, 0, 3, 4, [prom(f'ts_stream_bitrate_bps{{{s},type="total"}}')], "bps",
                  steps=[("blue", None)]))
    P.append(stat("Битрейт net", 6, 0, 3, 4, [prom(f'ts_stream_bitrate_bps{{{s},type="net"}}')], "bps",
                  steps=[("blue", None)], desc="Без нулевых пакетов (PID 0x1FFF)"))
    P.append(stat("CC-ошибки за период", 9, 0, 3, 4,
                  [prom(f"sum(increase(ts_stream_cc_errors_total{{{s}}}[$__range]))", instant=True)],
                  steps=[("green", None), ("orange", 1), ("red", 100)], decimals=0,
                  desc="Разрывы continuity counter по всем PID за выбранный период"))
    P.append(stat("TEI за период", 12, 0, 3, 4,
                  [prom(f"sum(increase(ts_stream_transport_errors_total{{{s}}}[$__range]))", instant=True)],
                  steps=[("green", None), ("red", 1)], decimals=0,
                  desc="Пакеты с transport_error_indicator"))
    P.append(stat("Джиттер IAT (std.dev)", 15, 0, 3, 4,
                  [prom(f'ts_stream_iat_seconds{{{s},stat="stddev"}}')], "s",
                  steps=[("green", None), ("orange", 0.002), ("red", 0.01)],
                  desc="Стандартное отклонение интервала между UDP-датаграммами (плагин iat)"))
    P.append(stat("IAT max", 18, 0, 3, 4,
                  [prom(f'ts_stream_iat_seconds{{{s},stat="max"}}')], "s",
                  steps=[("green", None), ("orange", 0.01), ("red", 0.05)],
                  desc="Наибольший интервал между UDP-датаграммами за 5 с"))
    P.append(stat("PCR > 5 мс за период", 21, 0, 3, 4,
                  [prom(f"sum(increase(ts_stream_pcr_jitter_exceeded_total{{{s}}}[$__range]))", instant=True)],
                  steps=[("green", None), ("orange", 1), ("red", 100)], decimals=0,
                  desc="PCR, пришедшие с джиттером больше порога pcr_jitter_max (pcrverify --input-synchronous)"))

    # --- Сервис, PID, таблицы ---
    P.append(table("Сервис", 0, 4, 14, 3, [prom(f"ts_stream_service_info{{{s}}}", instant=True, fmt="table")],
                   rename={"service_name": "Сервис", "provider": "Провайдер", "service_type": "Тип"},
                   hide=("Value", "stream", "description")))
    P.append(table("PID", 0, 7, 14, 8, [prom(
        f"ts_stream_pid_bitrate_bps{{{s}}} * on(stream, pid) group_left(codec, language, resolution) "
        f"ts_stream_pid_info{{{s}}}", instant=True, fmt="table")],
        rename={"pid": "PID", "type": "Тип", "codec": "Кодек", "language": "Язык",
                "resolution": "Разрешение", "Value": "Битрейт"},
        hide=("stream", "description"),
        overrides=[override("Битрейт", ("unit", "bps"))], sort=("PID", False)))
    tbl = panel("bargauge", "Интервалы таблиц PSI/SI (max за 5 с)", 14, 4, 10, 11,
                [prom(f'ts_stream_table_interval_seconds{{{s},stat="max"}}', "{{table}}", instant=True)], "s",
                desc="Наибольший интервал повторения за последний отчёт. ETR 290: PAT, PMT, CAT ≤ 0.5 с; SDT ≤ 2 с; NIT ≤ 10 с")
    tbl["fieldConfig"]["defaults"]["thresholds"] = thresholds(("green", None), ("red", 0.5))
    tbl["fieldConfig"]["defaults"]["min"] = 0
    tbl["fieldConfig"]["overrides"] = [
        override("SDT", ("thresholds", thresholds(("green", None), ("red", 2)))),
        override("NIT", ("thresholds", thresholds(("green", None), ("red", 10)))),
    ]
    tbl["options"] = {"displayMode": "basic", "orientation": "horizontal", "showUnfilled": True,
                      "valueMode": "color", "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}}
    P.append(tbl)

    y = 15
    P.append(row("Битрейт", y)); y += 1
    P.append(timeseries("Битрейт потока", 0, y, 12, 8, [
        prom(f'ts_stream_bitrate_bps{{{s},type="total"}}', "TS", "A"),
        prom(f'ts_stream_bitrate_bps{{{s},type="net"}}', "net", "B")], "bps"))
    P.append(timeseries("Битрейт по PID", 12, y, 12, 8, [prom(
        f"ts_stream_pid_bitrate_bps{{{s}}} * on(stream, pid) group_left(codec, language) ts_stream_pid_info{{{s}}}",
        "{{pid}} {{codec}} {{language}}")], "bps", stack=True))
    y += 8

    P.append(row("Ошибки", y)); y += 1
    P.append(timeseries("CC-ошибки по PID (за минуту)", 0, y, 12, 8, [prom(
        f"sum by (pid) (increase(ts_stream_cc_errors_total{{{s}}}[1m])) > 0", "PID {{pid}}", interval="1m")],
        "short", bars=True, stack=True,
        desc="Разрывы continuity counter. Начало и конец каждой серии ошибок — в журнале событий ниже"))
    P.append(timeseries("TEI и sync (за минуту)", 12, y, 12, 8, [
        prom(f"increase(ts_stream_transport_errors_total{{{s}}}[1m])", "TEI", "A", interval="1m"),
        prom(f"increase(ts_stream_sync_errors_total{{{s}}}[1m])", "sync", "B", interval="1m")],
        "short", bars=True))
    y += 8

    P.append(row("Джиттер", y)); y += 1
    P.append(timeseries("IAT — интервал между UDP-датаграммами", 0, y, 12, 8, [
        prom(f'ts_stream_iat_seconds{{{s},stat="mean"}}', "среднее", "A"),
        prom(f'ts_stream_iat_seconds{{{s},stat="stddev"}}', "std.dev (джиттер)", "B"),
        prom(f'ts_stream_iat_seconds{{{s},stat="max"}}', "max", "C")], "s",
        desc="Плагин iat, время приёма ядра. Рост std.dev и max — неравномерная доставка (сеть или источник)"))
    pcr = timeseries("PCR-джиттер", 12, y, 12, 8, [
        prom(f"increase(ts_stream_pcr_jitter_exceeded_total{{{s}}}[1m])", "PCR > 5 мс за минуту", "A", interval="1m"),
        prom(f"ts_stream_pcr_jitter_max_seconds{{{s}}}", "max джиттер", "B")], "short",
        desc="pcrverify --input-synchronous: отклонение PCR от времени прихода пакета больше порога")
    pcr["fieldConfig"]["overrides"] = [
        override("max джиттер", ("unit", "s"), ("custom.axisPlacement", "right"), ("custom.drawStyle", "points")),
        override("PCR > 5 мс за минуту", ("custom.drawStyle", "bars"), ("custom.fillOpacity", 60))]
    P.append(pcr)
    y += 8

    P.append(row("Таблицы PSI/SI", y)); y += 1
    P.append(timeseries("Интервал повторения таблиц (max)", 0, y, 24, 7, [prom(
        f'ts_stream_table_interval_seconds{{{s},stat="max"}}', "{{table}}")], "s",
        desc="ETR 290: PAT, PMT, CAT ≤ 0.5 с; SDT ≤ 2 с; NIT ≤ 10 с"))
    y += 7

    P.append(row("Реклама SCTE-35", y)); y += 1
    tl = panel("state-timeline", "Рекламный блок", 0, y, 12, 5,
               [prom(f"ts_stream_scte35_break_active{{{s}}}", "блок")],
               desc="1 — между событиями out и in (или до конца заявленной длительности)")
    tl["fieldConfig"]["defaults"]["mappings"] = [{"type": "value", "options": {
        "0": {"text": "эфир", "color": "transparent", "index": 0},
        "1": {"text": "реклама", "color": "blue", "index": 1}}}]
    tl["options"] = {"showValue": "never", "mergeValues": True, "rowHeight": 0.8,
                     "legend": {"showLegend": False}}
    P.append(tl)
    P.append(stat("Блоков за период", 12, y, 3, 5,
                  [prom(f'sum(increase(ts_stream_scte35_events_total{{{s},direction="out"}}[$__range]))', instant=True)],
                  steps=[("blue", None)], decimals=0, desc="Наступивших событий out (начало рекламы)"))
    P.append(stat("Длительность блока", 15, y, 3, 5,
                  [prom(f"ts_stream_scte35_break_duration_seconds{{{s}}}")], "s", steps=[("blue", None)],
                  desc="break_duration из последнего splice_insert out"))
    P.append(stat("Pre-roll out", 18, y, 3, 5,
                  [prom(f'ts_stream_scte35_preroll_seconds{{{s},direction="out"}}')], "s",
                  steps=[("red", None), ("orange", 2), ("green", 4)],
                  desc="За сколько до начала блока пришла метка (pre-roll последнего события out)"))
    P.append(stat("Последняя SCTE-35", 21, y, 3, 5,
                  [prom(f"time() - ts_stream_scte35_last_command_timestamp_seconds{{{s}}}")], "s",
                  steps=[("green", None), ("orange", 30), ("red", 300)],
                  desc="Сколько секунд назад приходила любая секция SCTE-35 (включая splice_null). Растёт — вставщик молчит"))
    y += 5

    P.append(row("Журнал событий (Loki)", y)); y += 1
    P.append(logs("События потока", 0, y, 12, 14,
                  f'{EVENTS}, stream="$stream", kind!="scte35"}} | json | line_format `{EVENT_LINE}`',
                  desc="CC-ошибки (начало/конец), смена PID и сервиса, пропадание/возврат потока, перезапуски tsp"))
    P.append(logs("Метки SCTE-35", 12, y, 12, 14,
                  f'{EVENTS}, stream="$stream", kind="scte35"}} | json | line_format `{SCTE_LINE}`',
                  desc="splice_insert / time_signal и события out/in (pending — анонс, occurred — наступило). splice_null не пишется"))

    def ann(name, color, expr, text, tags=""):
        return {"name": name, "enable": True, "iconColor": color, "datasource": LOKI,
                "target": {"expr": expr, "refId": "A"}, "textFormat": text, "tagKeys": tags,
                "titleFormat": name}

    return {
        "uid": "ts-stream-details", "title": "TS Stream Details", "tags": ["stream-details", "ts-monitoring"],
        "schemaVersion": 42, "refresh": "30s", "time": {"from": "now-6h", "to": "now"},
        "graphTooltip": 1,
        "links": [{"title": "← Назад к обзору", "type": "link", "url": "/d/ts-stream-overview",
                   "icon": "external link", "keepTime": True}],
        "templating": {"list": [
            DS_VAR,
            {"name": "stream", "label": "Поток", "type": "query", "datasource": PROM,
             "query": {"query": f"label_values(ts_stream_status{{{JOB}}}, stream)", "refId": "A"},
             "refresh": 1, "sort": 1, "includeAll": False, "multi": False},
            {"name": "description", "label": "Канал", "type": "query", "datasource": PROM, "hide": 0,
             "query": {"query": f'label_values(ts_stream_status{{{JOB}, stream="$stream"}}, description)', "refId": "A"},
             "refresh": 2, "includeAll": False, "multi": False},
        ]},
        "annotations": {"list": [
            {"builtIn": 1, "datasource": {"type": "grafana", "uid": "-- Grafana --"}, "enable": True,
             "hide": True, "iconColor": "rgba(0, 211, 255, 1)", "name": "Annotations & Alerts", "type": "dashboard"},
            ann("Реклама SCTE-35", "blue",
                f'{EVENTS}, stream="$stream", kind="scte35"}} | json | type="event" | progress="occurred"',
                "{{event_type}} id={{event_id}}", "event_type"),
            ann("CC-ошибки", "red",
                f'{EVENTS}, stream="$stream", kind="cc_errors"}} | json | phase="start"', "CC-ошибки начались"),
            ann("Смена PID / сервиса", "orange",
                f'{EVENTS}, stream="$stream", kind=~"pids_changed|service_changed"}}', "{{kind}}"),
            ann("Поток пропал / вернулся", "purple",
                f'{EVENTS}, stream="$stream", kind=~"stream_down|stream_up"}}', "{{kind}}"),
        ]},
        "panels": P,
    }


def overview():
    P = []
    P.append(stat("📊 Статистика потоков", 0, 0, 24, 3, [
        prom(f"count(ts_stream_status{{{JOB}}})", "Всего", "A"),
        prom(f"count(ts_stream_status{{{JOB}}} == 1)", "Online", "B"),
        prom(f"count(ts_stream_status{{{JOB}}} == 0) or vector(0)", "Offline", "C"),
        prom(f"count(sum by (stream) (increase(ts_stream_cc_errors_total{{{JOB}}}[5m])) > 0) or vector(0)",
             "С CC-ошибками (5 мин)", "D"),
        prom(f"count(ts_stream_scte35_break_active{{{JOB}}} == 1) or vector(0)", "Идёт реклама", "E"),
        prom("sum(increase(ts_host_udp_rcvbuf_errors_total[5m])) or vector(0)", "UDP-потери на сервере (5 мин)", "F"),
    ], text_mode="value_and_name", steps=[("blue", None)]))
    P[-1]["fieldConfig"]["overrides"] = [
        override("Online", ("color", {"mode": "fixed", "fixedColor": "green"})),
        override("Offline", ("thresholds", thresholds(("green", None), ("red", 1)))),
        override("С CC-ошибками (5 мин)", ("thresholds", thresholds(("green", None), ("orange", 1)))),
        override("UDP-потери на сервере (5 мин)", ("thresholds", thresholds(("green", None), ("red", 1))),
                 ("decimals", 0),
                 ("description", "Датаграммы, выброшенные ядром 192.168.1.26 из-за переполнения буфера сокета — "
                                  "потеря на нашем приёме, а не в сети")),
    ]

    # Мозаика: 0 OFFLINE, 1 ONLINE, 2 — online, но были CC-ошибки за 5 минут
    mosaic = stat("🟩 Статус TS потоков — КЛИКНИТЕ НА КВАДРАТ для деталей", 0, 3, 24, 16, [prom(
        f"(ts_stream_status{{{JOB}}} + on(stream) group_left() "
        f"clamp_max(sum by (stream) (increase(ts_stream_cc_errors_total{{{JOB}}}[5m])), 1)) "
        f"or ts_stream_status{{{JOB}}}",
        "{{description}} | {{stream}}")],
        steps=[("red", None), ("green", 1), ("orange", 2)], mappings=STATUS_MAPPINGS, text_mode="value_and_name",
        desc="Зелёный — online, оранжевый — online, но за 5 минут были CC-ошибки, красный — offline")
    mosaic["fieldConfig"]["defaults"]["links"] = [{
        "title": "${__field.labels.description}", "targetBlank": False,
        "url": "/d/ts-stream-details?var-stream=${__field.labels.stream}"
               "&var-description=${__field.labels.description}&from=now-6h&to=now"}]
    P.append(mosaic)

    offline = row("🔴 OFFLINE потоки (кликните чтобы развернуть)", 19, collapsed=True)
    offline["panels"] = [table("🔴 Список OFFLINE потоков", 0, 20, 24, 8,
                               [prom(f"ts_stream_status{{{JOB}}} == 0", instant=True, fmt="table")],
                               rename={"description": "Название канала", "stream": "IP:Port"},
                               hide=("Value",), overrides=[stream_link_override()])]
    P.append(offline)


    y = 20
    P.append(row("Проблемы", y)); y += 1
    sel = JOB
    problems = table("Проблемные потоки за час", 0, y, 14, 10, [
        prom(f"sum by (stream, description) (increase(ts_stream_cc_errors_total{{{sel}}}[1h]))", ref="A",
             instant=True, fmt="table"),
        prom(f"sum by (stream, description) (increase(ts_stream_transport_errors_total{{{sel}}}[1h]))", ref="B",
             instant=True, fmt="table"),
        prom(f"sum by (stream, description) (increase(ts_stream_pcr_jitter_exceeded_total{{{sel}}}[1h]))", ref="C",
             instant=True, fmt="table"),
        prom(f'max by (stream, description) (ts_stream_iat_seconds{{{sel},stat="stddev"}})', ref="D",
             instant=True, fmt="table"),
        prom(f'max by (stream, description) (ts_stream_iat_seconds{{{sel},stat="max"}})', ref="E",
             instant=True, fmt="table"),
    ], rename={"description": "Канал", "stream": "IP:Port", "Value #A": "CC", "Value #B": "TEI",
               "Value #C": "PCR > 5 мс", "Value #D": "IAT std.dev", "Value #E": "IAT max"},
        transformations=[
            {"id": "merge", "options": {}},
            {"id": "filterByValue", "options": {"type": "include", "match": "any", "filters": [
                {"fieldName": "Value #A", "config": {"id": "greater", "options": {"value": 0}}},
                {"fieldName": "Value #B", "config": {"id": "greater", "options": {"value": 0}}},
                {"fieldName": "Value #C", "config": {"id": "greater", "options": {"value": 0}}},
                {"fieldName": "Value #D", "config": {"id": "greater", "options": {"value": 0.002}}},
            ]}},
        ],
        overrides=[stream_link_override(),
                   override("CC", ("decimals", 0), ("custom.cellOptions", {"type": "color-text"}),
                            ("thresholds", thresholds(("text", None), ("orange", 1), ("red", 100)))),
                   override("TEI", ("decimals", 0)),
                   override("PCR > 5 мс", ("decimals", 0)),
                   override("IAT std.dev", ("unit", "s"), ("custom.cellOptions", {"type": "color-text"}),
                            ("thresholds", thresholds(("text", None), ("orange", 0.002), ("red", 0.01)))),
                   override("IAT max", ("unit", "s"))],
        sort=("CC", True),
        desc="Потоки с CC-ошибками, TEI или превышениями PCR за час, либо с IAT std.dev > 2 мс. Клик по IP:Port — страница потока")
    P.append(problems)
    P.append(table("Сейчас идёт реклама (SCTE-35)", 14, y, 10, 10, [
        prom(f"ts_stream_scte35_break_active{{{JOB}}} == 1", instant=True, fmt="table", ref="A")],
        rename={"description": "Канал", "stream": "IP:Port"}, hide=("Value",),
        overrides=[stream_link_override()]))
    y += 10

    P.append(logs("События всех потоков", 0, y, 24, 12,
                  f'{EVENTS}, kind!="scte35"}} | json | line_format `{{{{ .description }}}} | {EVENT_LINE}`',
                  desc="CC-ошибки, смена PID и сервисов, пропадание/возврат потоков, перезапуски tsp"))
    y += 12

    P.append(row("Графики", y, collapsed=True))
    P[-1]["panels"] = [
        timeseries("Битрейт всех потоков", 0, y + 1, 24, 10, [prom(
            f'ts_stream_bitrate_bps{{{JOB},type="total"}}', "{{description}} - {{stream}}")], "bps",
            legend_calcs=()),
        timeseries("CC-ошибки (за минуту)", 0, y + 11, 24, 10, [prom(
            f"sum by (stream, description) (increase(ts_stream_cc_errors_total{{{JOB}}}[1m])) > 0",
            "{{description}} - {{stream}}", interval="1m")], "short", bars=True, stack=True),
        timeseries("UDP-потери на сервере (за минуту)", 0, y + 21, 24, 6, [prom(
            "increase(ts_host_udp_rcvbuf_errors_total[1m])", "RcvbufErrors", interval="1m")], "short", bars=True),
    ]

    return {
        "uid": "ts-stream-overview", "title": "TS Stream Monitoring - Overview", "tags": ["streams", "ts-monitoring"],
        "schemaVersion": 42, "refresh": "30s", "time": {"from": "now-1h", "to": "now"},
        "templating": {"list": [DS_VAR]},
        "annotations": {"list": [{"builtIn": 1, "datasource": {"type": "grafana", "uid": "-- Grafana --"},
                                  "enable": True, "hide": True, "iconColor": "rgba(0, 211, 255, 1)",
                                  "name": "Annotations & Alerts", "type": "dashboard"}]},
        "panels": P,
    }


def push(dash, message):
    token = open(TOKEN_FILE).read().strip()
    body = json.dumps({"dashboard": dash, "overwrite": True, "folderUid": "", "message": message}).encode()
    req = urllib.request.Request(f"{GRAFANA}/api/dashboards/db", data=body, method="POST",
                                 headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"})
    with urllib.request.urlopen(req) as r:
        res = json.load(r)
    print(f"{dash['uid']}: {res.get('status')} version {res.get('version')} {GRAFANA}{res.get('url')}")


def main():
    dashboards = [overview(), details()]
    for d in dashboards:
        with open(os.path.join(HERE, d["uid"] + ".json"), "w") as f:
            json.dump(d, f, ensure_ascii=False, indent=2)
            f.write("\n")
    if "--push" in sys.argv:
        for d in dashboards:
            push(d, "tsmonitor v2: jitter, SCTE-35, event log (build.py)")


if __name__ == "__main__":
    main()
