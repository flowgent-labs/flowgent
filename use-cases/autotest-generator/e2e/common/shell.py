"""
Flowgent E2E — shell command runner.
"""

import subprocess
import os


def run_cmd(cmd, timeout=120, env=None, cwd=None):
    """Run a command, stream output. Returns (returncode, stdout_str)."""
    print(f"  $ {' '.join(cmd)}")
    merged_env = {**os.environ, **(env or {})}
    try:
        result = subprocess.run(
            cmd, timeout=timeout, env=merged_env, cwd=cwd,
            capture_output=True, text=True,
        )
        if result.stdout:
            for line in result.stdout.splitlines():
                print(f"    {line}")
        if result.stderr:
            for line in result.stderr.splitlines():
                print(f"    [stderr] {line}")
        return result.returncode, result.stdout.strip()
    except subprocess.TimeoutExpired:
        print(f"    ERROR: timed out after {timeout}s")
        return 1, ""
    except FileNotFoundError:
        print(f"    ERROR: command not found: {cmd[0]}")
        return 1, ""
