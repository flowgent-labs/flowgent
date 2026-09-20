"""Scenario 22 — encrypted notification CRUD and real MQTT→HTTP delivery."""
from __future__ import annotations

import base64
import json
import os
import subprocess
import time
import uuid

import requests
from common import project as common_api

from common import config
from common.project import FlowgentE2EProject

API = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID
SYSTEM_NAMESPACE = config.SYSTEM_NAMESPACE
EMQX = config.EMQX_HOST
EMQX_PORT = config.EMQX_PORT
CHANNEL_NAME = "security-autonomy-alerts"
RECEIVER = f"{config.RESOURCE_PREFIX}-webhook"


class NotifierOperations:
    """Class-owned operations for s22 notifier."""

    @staticmethod
    def _decode_delivery(payload):
        value = json.loads(payload.decode())
        encoded = value.get("payload") if isinstance(value, dict) else None
        if encoded:
            value = json.loads(base64.b64decode(encoded).decode())
        return value

    @staticmethod
    def _receiver_receipts():
        if os.getenv("FLOWGENT_E2E_DEPLOYER") == "docker":
            response = requests.get("http://127.0.0.1:21081/receipts", timeout=10)
            response.raise_for_status()
            return response.json()
        pod_response = subprocess.run(
            [
                "kubectl", "get", "pod", "-n", SYSTEM_NAMESPACE,
                "-l", f"app={RECEIVER}", "--field-selector=status.phase=Running", "-o", "json",
            ],
            capture_output=True, text=True, timeout=20, check=True,
        )
        candidates = []
        for item in json.loads(pod_response.stdout).get("items", []):
            ready = any(
                condition.get("type") == "Ready" and condition.get("status") == "True"
                for condition in item.get("status", {}).get("conditions", [])
            )
            if ready:
                candidates.append(item)
        if not candidates:
            raise AssertionError("ready notification receiver pod not found")
        candidates.sort(key=lambda item: item.get("status", {}).get("startTime", ""))
        pod = candidates[-1]["metadata"]["name"]
        result = subprocess.run(
            [
                "kubectl", "exec", "-n", SYSTEM_NAMESPACE, pod, "--",
                "curl", "-fsS", "http://127.0.0.1:8080/receipts",
            ],
            capture_output=True, text=True, timeout=20, check=True,
        )
        return json.loads(result.stdout)

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print("  Scenario 22: Encrypted Notifier — Real Delivery")
        print("=" * 60)

        session = common_api.FlowgentE2EProject.session()
        channels_response = session.get(
            f"{API}/api/v1/{NAMESPACE}/notifications/channels", timeout=10,
        )
        channels_response.raise_for_status()
        channels = channels_response.json().get("items", [])
        matches = [item for item in channels if item.get("name") == CHANNEL_NAME]
        if len(matches) != 1:
            raise AssertionError(f"expected one UI-provisioned {CHANNEL_NAME}, got {len(matches)}")
        channel = matches[0]
        channel_id = channel.get("id")
        configured = set(channel.get("configured_secret_fields") or [])
        if configured != {"url", "headers"}:
            raise AssertionError(f"configured secret fields mismatch: {sorted(configured)}")
        public_json = json.dumps(channel)
        token = os.getenv("FLOWGENT_E2E_NOTIFICATION_TOKEN", "")
        receiver_url = os.getenv("FLOWGENT_E2E_NOTIFICATION_URL", "")
        if token and token in public_json:
            raise AssertionError("notification token leaked through management API")
        if receiver_url and receiver_url in public_json:
            raise AssertionError("notification URL leaked through management API")
        print("  ✓ UI-provisioned channel is write-only/redacted")

        conn = FlowgentE2EProject.pg_connect()
        if conn is None:
            raise AssertionError("PostgreSQL connection is required for ciphertext verification")
        try:
            with conn.cursor() as cursor:
                cursor.execute("SELECT config::text FROM nfy_channel WHERE id=%s", (channel_id,))
                row = cursor.fetchone()
            if not row:
                raise AssertionError("notification DB row not found")
            stored = row[0]
            if token and token in stored:
                raise AssertionError("notification token stored in plaintext")
            if receiver_url and receiver_url in stored:
                raise AssertionError("notification URL stored in plaintext")
            if "ciphertext" not in stored or "__flowgent_sealed_secrets_v1" not in stored:
                raise AssertionError("notification DB config has no versioned ciphertext envelope")
        finally:
            conn.close()
        print("  ✓ DB contains a versioned authenticated ciphertext envelope")

        try:
            import paho.mqtt.client as mqtt
        except ImportError as exc:
            raise AssertionError("paho-mqtt is required for real notification E2E") from exc

        delivery_id = str(uuid.uuid4())
        result_topic = f"flowgent/v1/{NAMESPACE}/flows/notification-test/runs/{delivery_id}/notify/result"
        received = []

        def on_message(_client, _userdata, message):
            try:
                received.append(NotifierOperations._decode_delivery(message.payload))
            except Exception as exc:  # keep a diagnostic without printing payload/secrets
                received.append({"status": "DECODE_FAILED", "error": str(exc)})

        mqtt_client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2)
        mqtt_client.on_message = on_message
        mqtt_client.connect(EMQX, EMQX_PORT, 10)
        mqtt_client.subscribe(result_topic, qos=1)
        mqtt_client.loop_start()
        try:
            response = session.post(
                f"{API}/api/v1/{NAMESPACE}/notifications/test",
                json={
                    "channel_id": channel_id,
                    "delivery_id": delivery_id,
                    "title": "Security autonomy fixer E2E",
                    "message": "authenticated notification delivery",
                },
                timeout=10,
            )
            if response.status_code != 202:
                raise AssertionError(f"notification test returned {response.status_code}: {response.text[:160]}")
            deadline = time.time() + 20
            while time.time() < deadline and not received:
                time.sleep(0.2)
        finally:
            mqtt_client.loop_stop()
            mqtt_client.disconnect()

        if not received:
            raise AssertionError("no Notifier delivery result received over MQTT")
        result = received[-1]
        if result.get("delivery_id") != delivery_id or result.get("channel_id") != channel_id:
            raise AssertionError("delivery result correlation mismatch")
        if result.get("status") != "DELIVERED":
            raise AssertionError(f"real notification delivery failed: {result.get('error', 'unknown')}")
        print("  ✓ Notifier decrypted the DB secret and emitted DELIVERED")

        receipts = NotifierOperations._receiver_receipts()
        if int(receipts.get("authorized_count", 0)) < 1:
            raise AssertionError("authenticated webhook receiver has no accepted delivery")
        print("  ✓ Authenticated HTTP receiver accepted the real webhook")
        print("\n  ✓ All encrypted Notifier checks passed")







from common.model import RunContext, VerificationResult
from verifier import BaseVerifier


class NotifierVerifier(BaseVerifier):
    scenario_id = "22"
    title = "Notifier — Multi-Channel Delivery"

    def run(self) -> VerificationResult:
        return self.execute(lambda: self.step("verify redaction, ciphertext, MQTT, and webhook delivery", self._verify_delivery))

    @staticmethod
    def _verify_delivery() -> None:
        NotifierOperations._verify_scenario()

def verifier(context: RunContext) -> VerificationResult:
    """Run the notifier scenario."""
    return NotifierVerifier(context).run()
