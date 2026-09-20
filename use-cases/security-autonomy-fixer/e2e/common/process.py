"""Subprocess execution with deterministic output and command evidence."""

from pathlib import Path
import os
import shlex
import subprocess
import time

from .model import CommandResult


class CommandRunner:
    """Single command-execution boundary for deployers and verifiers."""

    @staticmethod
    def run(
        command: tuple[str, ...] | list[str],
        *,
        cwd: Path | str | None = None,
        environment: dict[str, str] | None = None,
        timeout_seconds: int = 120,
        stream: bool = True,
    ) -> CommandResult:
        """Execute one command and return the same evidence contract as AuthGuard."""
        command = tuple(str(part) for part in command)
        working_directory = Path(cwd or Path.cwd()).resolve()
        merged_environment = {**os.environ, **(environment or {})}
        started = time.monotonic()
        print(f"  $ {shlex.join(command)}", flush=True)
        try:
            completed = subprocess.run(
                command,
                timeout=timeout_seconds,
                env=merged_environment,
                cwd=working_directory,
                capture_output=True,
                text=True,
            )
            output = completed.stdout
            if completed.stderr:
                output += completed.stderr
            if stream and output:
                for line in output.rstrip().splitlines():
                    print(f"    {line}", flush=True)
            return_code = completed.returncode
        except subprocess.TimeoutExpired as error:
            output = "".join(part for part in (error.stdout, error.stderr) if isinstance(part, str))
            output += f"\nTimed out after {timeout_seconds} seconds."
            return_code = 124
            if stream:
                print(f"    ERROR: timed out after {timeout_seconds}s", flush=True)
        except OSError as error:
            output = str(error)
            return_code = 127
            if stream:
                print(f"    ERROR: {error}", flush=True)
        return CommandResult(
            command=command,
            cwd=working_directory,
            return_code=return_code,
            duration_seconds=time.monotonic() - started,
            output=output.strip(),
        )

    @classmethod
    def legacy(cls, command, timeout=120, env=None, cwd=None):
        """Temporary tuple-returning adapter for legacy component helpers."""
        result = cls.run(command, cwd=cwd, environment=env, timeout_seconds=timeout)
        return result.return_code, result.output
