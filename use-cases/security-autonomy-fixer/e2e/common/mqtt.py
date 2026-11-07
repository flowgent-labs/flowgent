"""MQTT probe clients shared by class-based E2E verifiers."""

from __future__ import annotations

import base64
import json
import time
import uuid
from typing import Any

try:
    import paho.mqtt.client as mqtt
except ImportError:
    mqtt = None


class ApiLifecycleMqttClient:
    """Collect API lifecycle events without coupling to an individual verifier."""

    def __init__(self, host: str, port: int) -> None:
        if mqtt is None:
            self.client = None
            self.messages: list[dict[str, Any]] = []
            return
        self.client = mqtt.Client()
        self.messages: list[dict[str, Any]] = []
        self.client.on_message = self._on_message
        try:
            self.client.connect(host, port, 60)
            self.client.loop_start()
        except Exception as error:
            print(f"  WARN: MQTT connection failed: {error}")
            self.client = None

    def _on_message(self, _client: Any, _userdata: Any, message: Any) -> None:
        try:
            self.messages.append(
                {
                    "topic": message.topic,
                    "payload": json.loads(message.payload.decode()),
                    "timestamp": time.time(),
                }
            )
        except Exception:
            return

    def subscribe(self, topic: str) -> None:
        if self.client:
            self.client.subscribe(topic)

    def wait_for_message(self, topic_pattern: str, timeout: int = 5) -> dict[str, Any] | None:
        if not self.client:
            return None
        deadline = time.time() + timeout
        while time.time() < deadline:
            for message in self.messages:
                if topic_pattern in message["topic"]:
                    self.messages.remove(message)
                    return message
            time.sleep(0.1)
        return None

    def close(self) -> None:
        if self.client:
            self.client.loop_stop()
            self.client.disconnect()


class FlowgentMqttClient:
    """Publish and observe Flowgent's real InterMessage MQTT envelopes."""

    def __init__(self, host: str, port: int) -> None:
        if mqtt is None:
            raise RuntimeError("paho-mqtt is required for MQTT E2E verification")
        self.client = mqtt.Client()
        self.messages: dict[str, list[dict[str, Any]]] = {}
        self.client.on_message = self._on_message
        try:
            self.client.connect(host, port, 60)
            self.client.loop_start()
            time.sleep(0.5)
            print(f"  Connected to MQTT broker: {host}:{port}")
        except Exception as error:
            raise RuntimeError(f"MQTT connection failed: {error}") from error

    def _on_message(self, _client: Any, _userdata: Any, message: Any) -> None:
        try:
            envelope = json.loads(message.payload.decode())
        except Exception:
            payload: Any = message.payload.decode()
        else:
            payload = envelope
            if isinstance(envelope, dict) and "payload" in envelope:
                try:
                    payload = json.loads(base64.b64decode(envelope["payload"]).decode())
                except Exception:
                    pass
        self.messages.setdefault(message.topic, []).append(
            {"payload": payload, "timestamp": time.time()}
        )

    def subscribe(self, topic: str) -> None:
        self.client.subscribe(topic)

    def publish(self, topic: str, payload: dict[str, Any], envelope_id: str | None = None) -> None:
        encoded = json.dumps(payload).encode()
        envelope = {
            "id": envelope_id or str(uuid.uuid4())[:8],
            "payload": base64.b64encode(encoded).decode(),
        }
        self.client.publish(topic, json.dumps(envelope), qos=1)

    def wait_for_message(self, topic_filter: str, timeout: int = 5) -> dict[str, Any] | None:
        match_filter = topic_filter
        if topic_filter.startswith("$share/"):
            parts = topic_filter.split("/", 2)
            match_filter = parts[2] if len(parts) > 2 else topic_filter
        deadline = time.time() + timeout
        while time.time() < deadline:
            for topic, messages in self.messages.items():
                if messages and (
                    match_filter in topic or self._topic_matches(match_filter, topic)
                ):
                    return {"topic": topic, **messages.pop(0)}
            time.sleep(0.1)
        return None

    @staticmethod
    def _topic_matches(pattern: str, topic: str) -> bool:
        pattern_parts = pattern.split("/")
        topic_parts = topic.split("/")
        return len(pattern_parts) == len(topic_parts) and all(
            expected == "+" or expected == actual
            for expected, actual in zip(pattern_parts, topic_parts)
        )

    def clear_messages(self) -> None:
        self.messages = {}

    def close(self) -> None:
        self.client.loop_stop()
        self.client.disconnect()


__all__ = ["ApiLifecycleMqttClient", "FlowgentMqttClient"]
