#!/usr/bin/env python3
import copy
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.parse
from pathlib import Path

import yaml


def _require_env(name):
    """目标机安全修正：敏感变量不再提供非空默认值，缺失即失败退出。"""
    v = os.environ.get(name)
    if not v:
        raise SystemExit(
            "缺少必需环境变量 %s；请在 /etc/sub2api-vless-relay.env 中提供（不得使用脚本内默认值）。" % name
        )
    return v


SUBSCRIPTION_URL = _require_env("VLESS_RELAY_UPSTREAM_URL")
PUBLIC_HOST = _require_env("VLESS_RELAY_PUBLIC_HOST")
SUBSCRIPTION_PORT = int(os.environ.get("VLESS_RELAY_SUB_PORT", "24480"))
PUBLIC_PORT_START = int(os.environ.get("VLESS_RELAY_PORT_START", "24443"))
PUBLIC_PORT_COUNT = int(os.environ.get("VLESS_RELAY_PORT_COUNT", "30"))
LOCAL_SOCKS_START = int(os.environ.get("VLESS_RELAY_LOCAL_SOCKS_START", "10808"))
SNI = os.environ.get("VLESS_RELAY_SNI", "www.microsoft.com")
STATE_DIR = Path(os.environ.get("VLESS_RELAY_STATE_DIR", "/home/ubuntu/sub2api/.dev/vless-relay"))
SING_BOX_DIR = Path(os.environ.get("VLESS_RELAY_SING_BOX_DIR", "/home/ubuntu/sub2api/.dev/sing-box"))
COMPOSE_DIR = Path(os.environ.get("VLESS_RELAY_COMPOSE_DIR", "/opt/sub2api/deploy"))
# 目标机迁移修正：compose 文件必须可通过 env 显式指定，不再硬编码 override.yml。
# VLESS_RELAY_COMPOSE_FILES 为冒号分隔的文件列表；相对路径基于 COMPOSE_DIR 解析。
# 默认使用目标机专用的 migration/docker-compose.target.yml，绝不加载 docker-compose.override.yml。
_compose_files_env = os.environ.get(
    "VLESS_RELAY_COMPOSE_FILES",
    "docker-compose.yml:migration/docker-compose.target.yml",
)
COMPOSE_FILES = [
    str(Path(f) if os.path.isabs(f) else COMPOSE_DIR / f)
    for f in _compose_files_env.split(":")
    if f
]
SING_BOX_CONTAINER = os.environ.get("VLESS_RELAY_SING_BOX_CONTAINER", "sub2api-sing-box-vless")
SUB_CONTAINER = os.environ.get("VLESS_RELAY_SUB_CONTAINER", "sub2api-vless-relay-sub")
LEGACY_SUBSCRIPTION_FILES = [
    name
    for name in os.environ.get("VLESS_RELAY_LEGACY_SUB_FILES", "5cd610b87766cfeb96d68d13633d17c6.yaml").split(",")
    if name
]
REALITY_PRIVATE_KEY = _require_env("VLESS_RELAY_REALITY_PRIVATE_KEY")
REALITY_PUBLIC_KEY = _require_env("VLESS_RELAY_REALITY_PUBLIC_KEY")


def run(cmd, *, check=True, capture=False):
    result = subprocess.run(
        cmd,
        check=False,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None,
    )
    if check and result.returncode != 0:
        if capture and result.stdout:
            print(result.stdout, file=sys.stderr)
        raise SystemExit(result.returncode)
    return result


def atomic_write(path: Path, content: str, mode: int):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", delete=False, dir=str(path.parent), encoding="utf-8") as f:
        f.write(content)
        tmp = Path(f.name)
    os.chmod(tmp, mode)
    os.replace(tmp, path)


def load_json(path: Path, default):
    if not path.exists():
        return default
    return json.loads(path.read_text())


def subscription_url_with_flag(flag: str) -> str:
    parsed = urllib.parse.urlsplit(SUBSCRIPTION_URL)
    query = [(k, v) for k, v in urllib.parse.parse_qsl(parsed.query, keep_blank_values=True) if k != "flag"]
    query.append(("flag", flag))
    return urllib.parse.urlunsplit(
        (parsed.scheme, parsed.netloc, parsed.path, urllib.parse.urlencode(query), parsed.fragment)
    )


def fetch_upstream():
    req = urllib.request.Request(
        subscription_url_with_flag("sing-box"),
        headers={"User-Agent": "sing-box/1.13.12"},
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        raw = resp.read()
    data = json.loads(raw.decode("utf-8", "replace"))
    nodes = [o for o in data.get("outbounds", []) if o.get("type") == "vless"]
    if not nodes:
        raise SystemExit("upstream returned no vless outbounds")
    meta_req = urllib.request.Request(
        subscription_url_with_flag("meta"),
        headers={"User-Agent": "clash.meta"},
    )
    with urllib.request.urlopen(meta_req, timeout=30) as resp:
        meta_raw = resp.read()
    meta_config = yaml.safe_load(meta_raw.decode("utf-8", "replace"))
    if not isinstance(meta_config, dict):
        raise SystemExit("upstream meta subscription is not a yaml mapping")
    by_tag = {o.get("tag"): o for o in nodes}
    ordered_nodes = []
    for proxy in meta_config.get("proxies") or []:
        if isinstance(proxy, dict) and proxy.get("name") in by_tag:
            ordered_nodes.append(by_tag[proxy["name"]])
    ordered_keys = {node_key(node) for node in ordered_nodes}
    ordered_nodes.extend(node for node in nodes if node_key(node) not in ordered_keys)
    nodes = ordered_nodes
    if len(nodes) > PUBLIC_PORT_COUNT:
        raise SystemExit(f"upstream has {len(nodes)} nodes, but only {PUBLIC_PORT_COUNT} public ports are configured")
    return raw, nodes, meta_raw, meta_config


def stable_uuid(seed: str) -> str:
    digest = hashlib.sha256(seed.encode()).digest()
    b = bytearray(digest[:16])
    b[6] = (b[6] & 0x0F) | 0x40
    b[8] = (b[8] & 0x3F) | 0x80
    return f"{b[0:4].hex()}-{b[4:6].hex()}-{b[6:8].hex()}-{b[8:10].hex()}-{b[10:16].hex()}"


def short_id(seed: str) -> str:
    return hashlib.sha256(seed.encode()).hexdigest()[:16]


def slugify(name: str, fallback: str) -> str:
    lowered = name.lower()
    out = []
    for ch in lowered:
        if ch.isalnum():
            out.append(ch)
        elif ch in "-_[]":
            out.append(ch)
        else:
            out.append("-")
    slug = "-".join(filter(None, "".join(out).split("-")))
    return slug[:60] or fallback


def node_key(node):
    tag = node.get("tag") or ""
    server = node.get("server") or ""
    port = node.get("server_port") or ""
    return f"{tag}|{server}|{port}"


def build(nodes, previous_state):
    mappings = previous_state.get("mappings", {})
    used_ports = set()
    node_states = []
    for idx, node in enumerate(nodes):
        key = node_key(node)
        old = mappings.get(key, {})
        port = old.get("public_port")
        if not isinstance(port, int) or not (PUBLIC_PORT_START <= port < PUBLIC_PORT_START + PUBLIC_PORT_COUNT) or port in used_ports:
            for candidate in range(PUBLIC_PORT_START, PUBLIC_PORT_START + PUBLIC_PORT_COUNT):
                if candidate not in used_ports and candidate not in {
                    v.get("public_port") for k, v in mappings.items() if k != key
                }:
                    port = candidate
                    break
            else:
                for candidate in range(PUBLIC_PORT_START, PUBLIC_PORT_START + PUBLIC_PORT_COUNT):
                    if candidate not in used_ports:
                        port = candidate
                        break
        used_ports.add(port)
        uuid = old.get("uuid") or stable_uuid(f"uuid:{key}")
        sid = old.get("short_id") or short_id(f"sid:{key}")
        tag = node.get("tag") or f"node-{idx + 1}"
        node_states.append(
            {
                "key": key,
                "tag": tag,
                "clash_name": tag,
                "slug": slugify(tag, f"node-{idx + 1}"),
                "uuid": uuid,
                "short_id": sid,
                "public_port": port,
                "local_port": LOCAL_SOCKS_START + idx,
                "upstream_tag": f"upstream-{idx + 1}",
                "local_inbound_tag": f"local-socks-{idx + 1}",
                "public_inbound_tag": f"public-vless-{idx + 1}",
                "node": node,
            }
        )
    return node_states


def yaml_quote(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def build_config(node_states):
    inbounds = []
    outbounds = [{"type": "direct", "tag": "direct"}]
    rules = []
    records = []
    for st in node_states:
        inbounds.append(
            {
                "type": "mixed",
                "tag": st["local_inbound_tag"],
                "listen": "0.0.0.0",
                "listen_port": st["local_port"],
            }
        )
        inbounds.append(
            {
                "type": "vless",
                "tag": st["public_inbound_tag"],
                "listen": "0.0.0.0",
                "listen_port": st["public_port"],
                "users": [{"uuid": st["uuid"], "flow": ""}],
                "tls": {
                    "enabled": True,
                    "server_name": SNI,
                    "reality": {
                        "enabled": True,
                        "handshake": {"server": SNI, "server_port": 443},
                        "private_key": REALITY_PRIVATE_KEY,
                        "short_id": [st["short_id"]],
                    },
                },
            }
        )
        upstream = dict(st["node"])
        upstream["tag"] = st["upstream_tag"]
        outbounds.append(upstream)
        rules.append({"inbound": [st["local_inbound_tag"], st["public_inbound_tag"]], "outbound": st["upstream_tag"]})
        records.append(
            {
                "name": "sing-box " + st["tag"],
                "protocol": "socks5h",
                "host": "sing-box-vless",
                "port": st["local_port"],
                "local_url": f"socks5h://127.0.0.1:{st['local_port']}",
                "container_url": f"socks5h://sing-box-vless:{st['local_port']}",
                "node_host": st["node"].get("server"),
            }
        )
    return (
        {
            "log": {"level": "info", "timestamp": True},
            "inbounds": inbounds,
            "outbounds": outbounds,
            "route": {"rules": rules, "final": "direct"},
        },
        records,
    )


def build_relay_proxy(st):
    proxy = {
        "name": st["clash_name"],
        "type": "vless",
        "server": PUBLIC_HOST,
        "port": st["public_port"],
        "uuid": st["uuid"],
        "alterId": 0,
        "cipher": "auto",
        "udp": True,
        "flow": "",
        "encryption": "none",
        "tls": True,
        "skip-cert-verify": False,
        "servername": SNI,
        "reality-opts": {
            "public-key": REALITY_PUBLIC_KEY,
            "short-id": st["short_id"],
        },
        "client-fingerprint": "chrome",
        "network": "tcp",
    }
    return proxy


def build_clash(node_states, upstream_clash):
    config = copy.deepcopy(upstream_clash)
    upstream_proxy_names = {
        proxy.get("name")
        for proxy in upstream_clash.get("proxies", []) or []
        if isinstance(proxy, dict) and proxy.get("type") == "vless"
    }
    relay_proxies = [build_relay_proxy(st) for st in node_states]
    relay_names = {proxy["name"] for proxy in relay_proxies}
    passthrough_proxies = [
        proxy
        for proxy in upstream_clash.get("proxies", []) or []
        if not (isinstance(proxy, dict) and proxy.get("name") in upstream_proxy_names)
    ]
    config["proxies"] = passthrough_proxies + relay_proxies

    # Keep upstream groups and rules intact. Only prune group references to nodes
    # that disappeared upstream so clients do not receive dangling proxy names.
    for group in config.get("proxy-groups", []) or []:
        if not isinstance(group, dict) or not isinstance(group.get("proxies"), list):
            continue
        group["proxies"] = [
            name
            for name in group["proxies"]
            if name in relay_names or name not in upstream_proxy_names
        ]
    return yaml.safe_dump(config, allow_unicode=True, sort_keys=False)


def build_fallback_clash(node_states):
    lines = [
        "mixed-port: 7890",
        "allow-lan: false",
        "mode: rule",
        "log-level: info",
        "external-controller: 127.0.0.1:9090",
        "proxies:",
    ]
    proxy_names = []
    for st in node_states:
        name = st["clash_name"]
        proxy_names.append(name)
        proxy = build_relay_proxy(st)
        lines.append("  - " + yaml.safe_dump([proxy], allow_unicode=True, sort_keys=False).split("\n", 1)[1].replace("\n", "\n    ").rstrip())
    lines.extend(
        [
            "proxy-groups:",
            '  - name: "Relay"',
            "    type: select",
            "    proxies:",
        ]
    )
    for name in proxy_names:
        lines.append(f"      - {yaml_quote(name)}")
    lines.extend(["      - DIRECT", "rules:", "  - MATCH,Relay", ""])
    return "\n".join(lines)


def file_changed(path: Path, content: str) -> bool:
    return not path.exists() or path.read_text() != content


def main():
    raw, nodes, meta_raw, upstream_clash = fetch_upstream()
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    (STATE_DIR / "sub").mkdir(parents=True, exist_ok=True)
    os.chmod(STATE_DIR, 0o700)
    state_path = STATE_DIR / "relay-state.json"
    previous_state = load_json(state_path, {})
    node_states = build(nodes, previous_state)
    config, records = build_config(node_states)
    sub_name = previous_state.get("subscription_file") or (hashlib.sha256(os.urandom(32)).hexdigest()[:32] + ".yaml")
    clash = build_clash(node_states, upstream_clash)
    desired_state = {
        "version": 1,
        "public_host": PUBLIC_HOST,
        "subscription_url": f"http://{PUBLIC_HOST}:{SUBSCRIPTION_PORT}/{sub_name}",
        "subscription_file": sub_name,
        "port_start": PUBLIC_PORT_START,
        "port_count": PUBLIC_PORT_COUNT,
        "local_socks_start": LOCAL_SOCKS_START,
        "reality_public_key": REALITY_PUBLIC_KEY,
        "sni": SNI,
        "mappings": {
            st["key"]: {
                "tag": st["tag"],
                "uuid": st["uuid"],
                "short_id": st["short_id"],
                "public_port": st["public_port"],
                "local_port": st["local_port"],
            }
            for st in node_states
        },
    }
    previous_comparable_state = dict(previous_state)
    previous_comparable_state.pop("updated_at", None)
    config_content = json.dumps(config, ensure_ascii=False, indent=2) + "\n"
    records_content = json.dumps(records, ensure_ascii=False, indent=2) + "\n"
    raw_path = SING_BOX_DIR / "subscription.last.raw"
    raw_meta_path = SING_BOX_DIR / "subscription.last.meta.yaml"
    config_path = SING_BOX_DIR / "config.json"
    records_path = SING_BOX_DIR / "proxy-records.json"
    sub_path = STATE_DIR / "sub" / sub_name
    legacy_sub_paths = [STATE_DIR / "sub" / name for name in LEGACY_SUBSCRIPTION_FILES if name != sub_name]
    changed = (
        file_changed(config_path, config_content)
        or file_changed(records_path, records_content)
        or file_changed(sub_path, clash)
        or any(file_changed(path, clash) for path in legacy_sub_paths)
        or previous_comparable_state != desired_state
        or not raw_path.exists()
        or raw_path.read_bytes() != raw
        or not raw_meta_path.exists()
        or raw_meta_path.read_bytes() != meta_raw
    )
    if not changed:
        print(json.dumps({"changed": False, "nodes": len(node_states), "subscription_url": desired_state["subscription_url"]}, ensure_ascii=False))
        return
    new_state = dict(desired_state)
    new_state["updated_at"] = int(time.time())
    state_content = json.dumps(new_state, ensure_ascii=False, indent=2) + "\n"
    ts = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    for path in (config_path, records_path, raw_path, raw_meta_path, state_path):
        if path.exists():
            shutil.copy2(path, path.with_name(path.name + f".bak.{ts}.sync"))
    atomic_write(config_path, config_content, 0o600)
    atomic_write(records_path, records_content, 0o600)
    atomic_write(raw_path, raw.decode("utf-8", "replace"), 0o600)
    atomic_write(raw_meta_path, meta_raw.decode("utf-8", "replace"), 0o600)
    atomic_write(sub_path, clash, 0o644)
    for path in legacy_sub_paths:
        atomic_write(path, clash, 0o644)
    atomic_write(STATE_DIR / "sub" / "index.html", "ok\n", 0o644)
    atomic_write(state_path, state_content, 0o600)
    check = run(["docker", "run", "--rm", "-v", f"{config_path}:/etc/sing-box/config.json:ro", "ghcr.io/sagernet/sing-box:latest", "check", "-c", "/etc/sing-box/config.json"], capture=True)
    compose_cmd = ["docker", "compose"]
    for file in COMPOSE_FILES:
        compose_cmd.extend(["-f", file])
    compose_cmd.extend(["up", "-d", "--no-deps", "--force-recreate", "sing-box-vless", "vless-relay-sub"])
    run(compose_cmd)
    print(json.dumps({"changed": True, "nodes": len(node_states), "subscription_url": new_state["subscription_url"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
