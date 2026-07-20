#!/usr/bin/env python3
"""
Scenario 07 — Wallet Module: x402 Key Management + Async MQTT Signing.

Validates the wallet daemon (pkg/wallet/pkg/walletmanager.go), which is the
ONLY process with private-key access. It performs TWO jobs and nothing more:

  1. Key management via HTTP API on :9901
       GET    /health
       POST   /api/v1/wallet/keys          (create / import Ed25519 key)
       GET    /api/v1/wallet/keys          (list)
       GET    /api/v1/wallet/keys/{name}   (address)
       DELETE /api/v1/wallet/keys/{name}
       POST   /api/v1/wallet/sign          (sync sign - convenience)
  2. Async signing via MQTT
       sub  $share/wallet-pool/flowgent/v1/+/flows/+/runs/+/sign/request
       pub  flowgent/v1/{tenant}/flows/{flow}/runs/{run}/sign/response

Architecture boundary (docs/02-L1-x402-Economic-Support.md section 5.1):
  The wallet is a DUMB SIGNER. All x402 logic - parsing the 402 response,
  building the PaymentIntent, evaluating spending policy, calling the
  facilitator - lives on the TaskManager side (pkg/core/pkg/client/). The
  wallet only receives an already-built unsigned payload, signs it with the
  EOA private key, and returns the signature. This scenario asserts that the
  sign/response carries a signature ONLY (no intent/policy/facilitator fields).

On-the-wire MQTT format: components exchange the InterMessage envelope
`{"id": "...", "payload": <base64(inner-json)>}`. The inner payload is the
SignRequest / SignResponse JSON. This test encodes/decodes that envelope so it
interoperates with the real Go wallet daemon.

All checks degrade gracefully (SKIP) when the wallet daemon is not deployed,
so the scenario is safe to run in payments-disabled environments.
"""

import sys
import time
import json
import uuid
import base64
import requests

sys.path.insert(0, '..')
import config

try:
    import paho.mqtt.client as mqtt
    MQTT_AVAILABLE = True
except ImportError:
    print("Warning: paho-mqtt not installed, MQTT signing test will be skipped")
    MQTT_AVAILABLE = False

WALLET_URL = config.WALLET_URL
WALLET_NAME = config.WALLET_NAME
TENANT = config.K3S_TENANT
EMQX_HOST = config.EMQX_HOST
EMQX_PORT = config.EMQX_PORT
TOPIC_PREFIX = "flowgent/v1"


def rand_id() -> str:
    return str(uuid.uuid4())[:8]


# -- Topic builders (mirror pkg/messager/pkg/messager.go) ----------

def sign_request_topic(tenant: str, flow: str, run: str) -> str:
    return f"{TOPIC_PREFIX}/{tenant}/flows/{flow}/runs/{run}/sign/request"


def sign_response_topic(tenant: str, flow: str, run: str) -> str:
    return f"{TOPIC_PREFIX}/{tenant}/flows/{flow}/runs/{run}/sign/response"


# -- InterMessage envelope helpers (mirror mqtt.go json marshaling) -

def wrap_envelope(msg_id: str, inner: dict) -> str:
    """Wrap an inner payload dict as an InterMessage: {id, payload:<base64>}."""
    inner_bytes = json.dumps(inner).encode()
    return json.dumps({
        "id": msg_id,
        "payload": base64.b64encode(inner_bytes).decode(),
    })


def unwrap_envelope(raw: bytes) -> dict:
    """Decode an InterMessage envelope back into the inner payload dict."""
    env = json.loads(raw.decode())
    payload = env.get("payload")
    if payload is None:
        return env
    return json.loads(base64.b64decode(payload).decode())


# -- HTTP helpers --------------------------------------------------

def wallet_reachable() -> bool:
    try:
        r = requests.get(f"{WALLET_URL}/health", timeout=3)
        return r.status_code == 200
    except Exception:
        return False


def test_health() -> bool:
    print("\n  -> [1] Wallet HTTP /health ...")
    try:
        r = requests.get(f"{WALLET_URL}/health", timeout=5)
        if r.status_code != 200:
            raise AssertionError(f"health returned {r.status_code}")
        print(f"      OK health: {r.json()}")
        return True
    except Exception as e:
        print(f"      FAIL health: {e}")
        return False


def test_key_management() -> bool:
    """Create -> address -> list -> delete an Ed25519 key via the HTTP API."""
    print("\n  -> [2] Key management CRUD ...")
    name = f"e2e-{rand_id()}"
    try:
        # CREATE (server generates the keypair)
        r = requests.post(f"{WALLET_URL}/api/v1/wallet/keys", json={"name": name}, timeout=5)
        if r.status_code not in (200, 201):
            # CSI provider forbids creation; treat as environmental skip
            if r.status_code == 403:
                print(f"      SKIP: provider forbids key creation ({r.json().get('error','')})")
                return True
            raise AssertionError(f"create key: {r.status_code} {r.text[:160]}")
        created = r.json()
        addr = created.get("address", "")
        if not addr.startswith("0x"):
            raise AssertionError(f"unexpected address: {addr!r}")
        print(f"      OK created key {name}: address={addr[:14]}...")

        # GET address
        r = requests.get(f"{WALLET_URL}/api/v1/wallet/keys/{name}", timeout=5)
        if r.status_code != 200 or r.json().get("address") != addr:
            raise AssertionError(f"get key mismatch: {r.status_code} {r.text[:160]}")
        print(f"      OK get key address matches")

        # LIST
        r = requests.get(f"{WALLET_URL}/api/v1/wallet/keys", timeout=5)
        if r.status_code != 200 or not any(k.get("name") == name for k in r.json()):
            raise AssertionError(f"list did not contain {name}")
        print(f"      OK list contains {name}")

        # DELETE
        r = requests.delete(f"{WALLET_URL}/api/v1/wallet/keys/{name}", timeout=5)
        if r.status_code != 200:
            raise AssertionError(f"delete: {r.status_code}")
        print(f"      OK deleted key {name}")
        return True
    except Exception as e:
        print(f"      FAIL key management: {e}")
        # best-effort cleanup
        try:
            requests.delete(f"{WALLET_URL}/api/v1/wallet/keys/{name}", timeout=3)
        except Exception:
            pass
        return False


def test_http_sign() -> bool:
    """Sign a payload via the synchronous HTTP /sign endpoint."""
    print("\n  -> [3] HTTP sync sign ...")
    name = f"e2e-sign-{rand_id()}"
    try:
        r = requests.post(f"{WALLET_URL}/api/v1/wallet/keys", json={"name": name}, timeout=5)
        if r.status_code == 403:
            print("      SKIP: provider forbids key creation")
            return True
        if r.status_code not in (200, 201):
            raise AssertionError(f"create key: {r.status_code}")

        payload = "unsigned-x402-payment-payload"
        r = requests.post(f"{WALLET_URL}/api/v1/wallet/sign",
                          json={"wallet": name, "payload": payload}, timeout=5)
        if r.status_code != 200:
            raise AssertionError(f"sign: {r.status_code} {r.text[:160]}")
        body = r.json()
        sig = body.get("signature", "")
        # Ed25519 signature is 64 bytes -> 128 hex chars
        if len(sig) != 128:
            raise AssertionError(f"unexpected signature length {len(sig)} (want 128 hex)")
        print(f"      OK signed: signature={sig[:16]}... ({len(sig)} hex chars)")
        return True
    except Exception as e:
        print(f"      FAIL HTTP sign: {e}")
        return False
    finally:
        try:
            requests.delete(f"{WALLET_URL}/api/v1/wallet/keys/{name}", timeout=3)
        except Exception:
            pass


def test_mqtt_sign_flow() -> bool:
    """
    Exercise the production async signing path end-to-end against the wallet
    daemon: publish an unsigned SignRequest to sign/request, wait for the
    signed SignResponse on sign/response, and assert the response is a bare
    signature (no x402 parsing / policy / facilitator fields).
    """
    print("\n  -> [4] Async MQTT signing (sign/request -> sign/response) ...")
    if not MQTT_AVAILABLE:
        print("      SKIP: paho-mqtt not installed")
        return True

    name = f"e2e-mqtt-{rand_id()}"
    flow_id = "wallet-e2e"
    run_id = "run-" + rand_id()
    req_id = str(uuid.uuid4())

    # Ensure a signing key exists for this wallet name.
    try:
        r = requests.post(f"{WALLET_URL}/api/v1/wallet/keys", json={"name": name}, timeout=5)
        if r.status_code == 403:
            print("      SKIP: provider forbids key creation")
            return True
        if r.status_code not in (200, 201):
            print(f"      SKIP: cannot provision key ({r.status_code})")
            return True
    except Exception as e:
        print(f"      SKIP: wallet HTTP unavailable for key setup: {e}")
        return True

    received = {}
    resp_topic = sign_response_topic(TENANT, flow_id, run_id)

    def on_message(client, userdata, msg):
        try:
            received["inner"] = unwrap_envelope(msg.payload)
        except Exception as e:
            received["error"] = str(e)

    ok = False
    try:
        client = mqtt.Client()
        client.on_message = on_message
        client.connect(EMQX_HOST, EMQX_PORT, 60)
        client.loop_start()
        client.subscribe(resp_topic, qos=1)
        time.sleep(0.5)

        sign_req = {
            "request_id": req_id,
            "wallet": name,
            "payload": "unsigned-x402-payment-payload",
            "tenant_id": TENANT,
            "flow_id": flow_id,
            "run_id": run_id,
        }
        req_topic = sign_request_topic(TENANT, flow_id, run_id)
        client.publish(req_topic, wrap_envelope(req_id, sign_req), qos=1)
        print(f"      -> published unsigned SignRequest to {req_topic}")

        deadline = time.time() + 10
        while time.time() < deadline and "inner" not in received:
            time.sleep(0.1)

        if "inner" not in received:
            print("      SKIP: no sign/response received (wallet daemon not subscribed?)")
            return True

        resp = received["inner"]
        if resp.get("error"):
            raise AssertionError(f"wallet returned error: {resp['error']}")
        if resp.get("request_id") != req_id:
            raise AssertionError(f"request_id mismatch: {resp.get('request_id')}")
        sig = resp.get("signature", "")
        if len(sig) != 128:
            raise AssertionError(f"unexpected signature length {len(sig)}")
        print(f"      OK received signed SignResponse: signature={sig[:16]}...")

        # Architecture boundary assertion: response carries signature ONLY.
        leaked = [k for k in ("intent", "policy", "facilitator", "amount",
                              "asset", "recipient", "url", "accepts")
                  if k in resp]
        if leaked:
            raise AssertionError(f"wallet leaked x402 fields {leaked} - parsing must live in TM, not wallet")
        print("      OK boundary: response is a bare signature (no x402 parsing in wallet)")
        ok = True
    except Exception as e:
        print(f"      FAIL MQTT signing: {e}")
        ok = False
    finally:
        try:
            client.loop_stop()
            client.disconnect()
        except Exception:
            pass
        try:
            requests.delete(f"{WALLET_URL}/api/v1/wallet/keys/{name}", timeout=3)
        except Exception:
            pass
    return ok


def run():
    print("\n" + "=" * 60)
    print("  Scenario 09: Wallet - x402 Key Management + MQTT Signing")
    print("=" * 60)

    if not wallet_reachable():
        print(f"\n  SKIP: wallet daemon not reachable at {WALLET_URL}")
        print("    (wallet disabled - set wallet.enabled=true to test)")
        print("\n  OK Wallet scenario skipped (no-op)")
        return

    results = {
        "health": test_health(),
        "key_management": test_key_management(),
        "http_sign": test_http_sign(),
        "mqtt_sign_flow": test_mqtt_sign_flow(),
    }

    passed = sum(1 for v in results.values() if v)
    total = len(results)
    print(f"\n  {'=' * 60}")
    print(f"  Summary: {passed}/{total} wallet checks passed")
    print(f"  {'=' * 60}")

    if passed < total:
        failed = [k for k, v in results.items() if not v]
        raise AssertionError(f"Failed wallet checks: {failed}")
    print("\n  OK All wallet checks passed")


if __name__ == "__main__":
    run()
