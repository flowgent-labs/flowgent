#!/usr/bin/env python3
"""Manual-access tunnels used by the unified E2E runner."""

import signal
import subprocess
import time

from common import config, run_cmd


def run():
    # Imported lazily to keep runner.py as the only command entry point.
    from runner import _PortForwards

    core = _PortForwards(config.SYSTEM_NAMESPACE, config.RELEASE_NAME).start()
    rc, envoy_service = run_cmd([
        "kubectl", "get", "service", "-n", config.SYSTEM_NAMESPACE,
        "-l", f"gateway.envoyproxy.io/owning-gateway-name={config.RESOURCE_PREFIX}-gateway",
        "-o", "jsonpath={.items[0].metadata.name}",
    ], timeout=20)
    if rc != 0 or not envoy_service.strip():
        core.stop()
        raise SystemExit("Envoy data-plane Service not found; run make e2e-security-fixer first")
    specs = (
        (f"{config.RESOURCE_PREFIX}-authguard-authn", config.LOCAL_AUTHN_PORT, 8082),
        (f"{config.RESOURCE_PREFIX}-authguard", config.LOCAL_AUTHZ_MGMT_PORT, 9091),
        (envoy_service.strip(), config.LOCAL_GATEWAY_PORT, 80),
    )
    processes = [
        subprocess.Popen(
            ["kubectl", "port-forward", "-n", config.SYSTEM_NAMESPACE,
             f"service/{service}", f"{local}:{remote}"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        for service, local, remote in specs
    ]
    try:
        time.sleep(2)
        if any(process.poll() is not None for process in processes):
            raise RuntimeError("one or more manual-access tunnels failed to bind")
        print("Flowgent/AuthGuard manual access is ready (Ctrl-C only closes tunnels):")
        print(f"  UI:              http://127.0.0.1:31080")
        print(f"  Flowgent API:    http://127.0.0.1:{config.LOCAL_API_PORT}")
        print(f"  Protected API:   http://127.0.0.1:{config.LOCAL_GATEWAY_PORT}")
        print(f"  AuthN:           http://127.0.0.1:{config.LOCAL_AUTHN_PORT}")
        print(f"  AuthZ management:http://127.0.0.1:{config.LOCAL_AUTHZ_MGMT_PORT}")
        signal.pause()
    except KeyboardInterrupt:
        pass
    finally:
        for process in processes:
            process.terminate()
        core.stop()
