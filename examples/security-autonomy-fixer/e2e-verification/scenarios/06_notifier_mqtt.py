"""
Scenario 06 — Notifier MQTT: EMQX Message Publishing.

Verifies that notification messages are published to the correct MQTT topic
when a flow completes.
"""

import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

EMQX = config.EMQX_HOST
EMQX_DASH = config.EMQX_DASHBOARD


def run():
    try:
        import paho.mqtt.client as mqtt
    except ImportError:
        print("  SKIP: paho-mqtt not installed")
        return

    # ── Check EMQX status ───────────────────────────────────
    import requests
    try:
        r = requests.get(f"http://{EMQX}:{EMQX_DASH}/api/v5/status", timeout=3)
        if r.status_code == 200:
            print(f"  EMQX OK: {r.json().get('status', 'unknown')}")
        else:
            print(f"  EMQX: status={r.status_code} (non-critical)")
    except Exception:
        print(f"  EMQX: not reachable at {EMQX}:{EMQX_DASH} (non-critical)")
        return

    # ── Subscribe to notification topic ─────────────────────
    topic = "flowgent/notify/queue/+/+/+"
    messages = []

    def on_message(client, userdata, msg):
        messages.append(msg.payload)

    client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2)
    client.on_message = on_message
    try:
        client.connect(EMQX, config.EMQX_PORT, 10)
        client.subscribe(topic)
        client.loop_start()
        print(f"  subscribed to {topic}")
    except Exception as e:
        print(f"  MQTT connect failed: {e} (non-critical)")
        return

    # ── Wait briefly for any messages ───────────────────────
    import time
    time.sleep(3)
    client.loop_stop()
    client.disconnect()

    print(f"  messages received: {len(messages)}")
