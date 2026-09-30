"""Exercise a fresh, isolated Compose world; never accepts production endpoints."""

from __future__ import annotations

import base64
import http.cookiejar
import json
import os
import socket
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from collections.abc import Callable
from functools import partial
from pathlib import Path
from typing import TypeVar

ROOT = Path(os.environ["EVIDENCE"])
COMPOSE = ["docker", "compose", "-f", str(Path(__file__).with_name("compose.yaml"))]
PORTS = {
    "app": 3000,
    "alloy": 12345,
    "prometheus": 9090,
    "loki": 3100,
    "tempo": 3200,
    "pyroscope": 4040,
    "grafana": 3000,
}
OPENER = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
)
T = TypeVar("T")


def object_map(value: object) -> dict[str, object]:
    assert isinstance(value, dict), "expected JSON object"
    result: dict[str, object] = {}
    for key, member in value.items():
        assert isinstance(key, str), "expected JSON string key"
        result[key] = member
    return result


def array(value: object) -> list[object]:
    assert isinstance(value, list), "expected JSON array"
    return list(value)


def field(value: object, *keys: str) -> object:
    for key in keys:
        value = object_map(value)[key]
    return value


def string(value: object) -> str:
    assert isinstance(value, str), "expected JSON string"
    return value


def decoded(raw: bytes) -> object:
    result: object = json.loads(raw)
    return result


def record(name: str, value: object) -> None:
    (ROOT / name).write_text(json.dumps(value, indent=2) + "\n")


def discover() -> dict[str, str]:
    result: dict[str, str] = {}
    for service, port in PORTS.items():
        container = subprocess.check_output(
            COMPOSE + ["ps", "-q", service], text=True
        ).strip()
        assert container, f"missing disposable {service} container"
        ip = subprocess.check_output(
            [
                "docker",
                "inspect",
                container,
                "--format",
                "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
            ],
            text=True,
        ).strip()
        assert ip.startswith("172."), "unexpected disposable bridge address"
        result[service] = f"http://{ip}:{port}"
    record("urls.json", result)
    return result


URLS = discover()


def request(
    service: str, path: str, data: object = None, trace_id: str = ""
) -> tuple[int, bytes]:
    headers = {"Content-Type": "application/json", "Origin": URLS["app"]}
    if trace_id:
        headers["traceparent"] = f"00-{trace_id}-1234567890123456-01"
    body = None if data is None else json.dumps(data).encode()
    req = urllib.request.Request(URLS[service] + path, data=body, headers=headers)
    try:
        with OPENER.open(req, timeout=10) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def json_request(service: str, path: str, data: object = None) -> object:
    status, body = request(service, path, data)
    assert status == 200, f"{service} {path}: HTTP {status}"
    return decoded(body)


def eventually(check: Callable[[], T], timeout: float = 240) -> T:
    deadline = time.monotonic() + timeout
    while True:
        try:
            return check()
        except (AssertionError, urllib.error.URLError, KeyError) as error:
            if time.monotonic() >= deadline:
                raise AssertionError(f"condition did not converge: {error}") from error
            time.sleep(1)


def metric(query: str) -> list[object]:
    response = json_request(
        "prometheus", "/api/v1/query?" + urllib.parse.urlencode({"query": query})
    )
    return array(field(response, "data", "result"))


def rule_states() -> list[object]:
    response = json_request("grafana", "/api/prometheus/grafana/api/v1/rules")
    return [
        rule
        for group in array(field(response, "data", "groups"))
        for rule in array(field(group, "rules"))
    ]


def normal_rules() -> list[object]:
    rules = rule_states()
    assert len(rules) == 7, "expected original rule plus six actionable rules"
    assert all(
        field(rule, "health") == "ok" and field(rule, "state") == "inactive"
        for rule in rules
    ), "rules not yet healthy and normal"
    return rules


def ready(service: str, path: str) -> None:
    status, _ = request(service, path)
    assert status == 200, f"{service} not ready: {status}"


def verify_signals() -> None:
    for service, path in {
        "app": "/health",
        "alloy": "/-/ready",
        "prometheus": "/-/ready",
        "loki": "/ready",
        "tempo": "/ready",
        "pyroscope": "/ready",
        "grafana": "/api/health",
    }.items():
        eventually(partial(ready, service, path))
        print(f"{service}: ready", flush=True)
    status, _ = request(
        "app",
        "/api/auth/login",
        {
            "username": "observability-proof",
            "password": os.environ["SVART_TEST_PASSWORD"],
        },
    )
    assert status == 200, "fixture login failed"
    status, _ = request(
        "app",
        "/api/rewrites",
        {
            "domain": "metrics.example.test",
            "ip_addresses": "192.0.2.20",
            "enabled": True,
        },
    )
    assert status == 201, "authenticated rewrite creation failed"
    status, _ = request(
        "app",
        "/api/blocklists",
        {
            "url": "http://fixture:8000/list.txt",
            "alias": "Telemetry fixture",
            "enabled": True,
        },
    )
    assert status == 201, "authenticated list creation failed"

    def list_loaded() -> object:
        lists = json_request("app", "/api/blocklists")
        assert any(
            field(item, "domain_count") == 2 for item in array(field(lists, "data"))
        )
        return lists

    record("downloaded-list.json", eventually(list_loaded))

    def list_trace() -> object:
        entries = [
            decoded(line.encode())
            for line in (ROOT / "logs/app.log").read_text().splitlines()
        ]
        completions = [
            entry for entry in entries if field(entry, "msg") == "refreshed list"
        ]
        assert completions, "list completion log has not arrived"
        trace_id = string(field(completions[-1], "trace_id"))
        data = json_request("tempo", "/api/traces/" + trace_id)
        spans = [
            span
            for batch in array(field(data, "batches"))
            for scope in array(field(batch, "scopeSpans"))
            for span in array(field(scope, "spans"))
        ]
        assert {field(span, "name") for span in spans} == {
            "list_download",
            "storage_read",
            "storage_write",
            "list_download HTTP",
        }, "incomplete real list trace hierarchy"
        parent = next(span for span in spans if field(span, "name") == "list_download")
        assert all(
            field(span, "parentSpanId") == field(parent, "spanId")
            for span in spans
            if span is not parent
        ), "storage/outbound span has the wrong parent"
        return data

    record("list-trace-hierarchy.json", eventually(list_trace))
    packet = struct.pack("!HHHHHH", 1234, 0x100, 1, 0, 0, 0)
    packet += b"\x07metrics\x07example\x04test\x00" + struct.pack("!HH", 1, 1)
    host = urllib.parse.urlparse(URLS["app"]).hostname
    assert host is not None
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.settimeout(3)
        for _ in range(400):
            sock.sendto(packet, (host, 5353))
            reply, _ = sock.recvfrom(4096)
            assert struct.unpack("!H", reply[2:4])[0] & 15 == 0
            assert struct.unpack("!H", reply[6:8])[0] == 1
    record("dns-replies.json", {"responses": 400, "rcode": 0, "answers": 1})
    trace_id = uuid.uuid4().hex
    status, _ = request(
        "app", "/docs?private_marker=must-not-export", trace_id=trace_id
    )
    assert status == 200
    for _ in range(150):
        status, _ = request("app", "/api/openapi.json")
        assert status == 200
    trace = eventually(lambda: json_request("tempo", "/api/traces/" + trace_id))
    batches = array(field(trace, "batches"))
    expected_version = (ROOT / "service-commit.txt").read_text().strip()
    attributes = {
        string(field(item, "key")): field(item, "value", "stringValue")
        for item in array(field(batches[0], "resource", "attributes"))
    }
    assert attributes["service.name"] == "svart-dns"
    assert attributes["service.version"] == expected_version
    spans = [
        span
        for batch in batches
        for scope in array(field(batch, "scopeSpans"))
        for span in array(field(scope, "spans"))
    ]
    assert len(spans) == 1 and field(spans[0], "name") == "GET /docs"
    assert base64.b64decode(string(field(spans[0], "traceId"))).hex() == trace_id
    record("trace.json", trace)
    query = '{service_name="svart-dns"} |= "' + trace_id + '"'

    def correlated_logs() -> object:
        data = json_request(
            "loki",
            "/loki/api/v1/query_range?"
            + urllib.parse.urlencode(
                {
                    "query": query,
                    "start": time.time_ns() - 600_000_000_000,
                    "end": time.time_ns(),
                    "limit": 100,
                }
            ),
        )
        assert array(field(data, "data", "result")), "correlated log has not arrived"
        return data

    logs = eventually(correlated_logs)
    assert "must-not-export" not in json.dumps(logs) + json.dumps(trace)
    record("correlated-logs.json", logs)
    (ROOT / "trace-id.txt").write_text(trace_id + "\n")
    for name, query in {
        "up": 'up{service_name="svart-dns"}',
        "http": 'svart_http_requests_total{route="/docs",status="200"}',
        "dns": "svart_dns_response_duration_seconds_count",
        "admission": 'svart_dns_log_admission_total{outcome="accepted"}',
        "queue": "svart_queue_pending_records",
        "jobs": "svart_background_operations_total",
    }.items():

        def fresh_metric(query: str = query, name: str = name) -> object:
            rows = metric(query)
            assert rows, f"missing metric {name}"
            for row in rows:
                value = array(field(row, "value"))
                assert (
                    float(
                        str(
                            array(
                                field(
                                    metric("time() - timestamp(" + query + ")")[0],
                                    "value",
                                )
                            )[1]
                        )
                    )
                    < 20
                ), "stale metric sample"
                if name == "up":
                    assert value[1] == "1", "scrape target is down"
            return rows

        record(name + "-metrics.json", eventually(fresh_metric))
    profile_request = {
        "start": str(int((time.time() - 600) * 1000)),
        "end": str(int(time.time() * 1000)),
        "matchers": ['{service_name="svart-dns"}'],
    }

    def profile_arrived() -> object:
        profile_request["end"] = str(int(time.time() * 1000))
        data = json_request(
            "pyroscope", "/querier.v1.QuerierService/Series", profile_request
        )
        values = [
            field(label, "value")
            for labels in array(field(data, "labelsSet"))
            for label in array(field(labels, "labels"))
        ]
        assert (
            "svart-dns" in values
            and "process_cpu:cpu:nanoseconds:cpu:nanoseconds" in values
            and expected_version in values
        )
        return data

    record("profiles.json", eventually(profile_arrived))
    record(
        "alert-rules.json", json_request("grafana", "/api/v1/provisioning/alert-rules")
    )
    record("alert-states.json", eventually(normal_rules))
    dashboard = json_request("grafana", "/api/dashboards/uid/svart-dns")
    assert len(array(field(dashboard, "dashboard", "panels"))) == 33
    record("dashboard.json", dashboard)
    lines = (ROOT / "logs/app.log").read_text().splitlines()
    for line in lines:
        entry = decoded(line.encode())
        if field(entry, "msg") in ("doom loop detected", "DNS doom loop detected"):
            assert "client_ip" not in object_map(entry) and "domain" not in object_map(
                entry
            )
    print(
        "Verified exact trace/log correlation, profiles, metrics and normal alerts",
        flush=True,
    )


def verify_no_data() -> None:
    subprocess.run(COMPOSE + ["stop", "alloy"], check=True, capture_output=True)
    status, _ = request(
        "prometheus",
        "/api/v1/admin/tsdb/delete_series?"
        + urllib.parse.urlencode({"match[]": 'up{service_name="svart-dns"}'}),
        {},
    )
    assert status == 204, "failed to remove synthetic scrape samples"

    def absent_fires() -> object:
        rules = rule_states()
        rule = next(
            rule for rule in rules if field(rule, "name") == "Svart metrics absent"
        )
        assert field(rule, "state") == "firing", "absence rule has not fired"
        return rule

    record("no-data-firing.json", eventually(absent_fires))
    subprocess.run(COMPOSE + ["start", "alloy"], check=True, capture_output=True)
    record("no-data-recovered.json", eventually(normal_rules))
    print("Verified missing scrape data fires the rule and fresh collection clears it")


if __name__ == "__main__":
    if "--no-data" in sys.argv:
        verify_no_data()
    else:
        verify_signals()
