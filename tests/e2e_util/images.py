"""Image provenance and registry selection shared by every deployment backend."""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path


class E2EImagesDefinitions:
    """Keep upstream/mirror pairs explicit; never infer a third-party image."""

    MIRROR = "registry.cn-shenzhen.aliyuncs.com/wl4g/"
    SONARQUBE_VERSION = "26.4.0.121862-community"
    MATRIX = {
        "prometheus": (
            "docker.io/prom/prometheus:v3.5.0",
            MIRROR + "prom_prometheus:v3.5.0",
        ),
        "pgvector": (
            "docker.io/pgvector/pgvector:pg16",
            MIRROR + "pgvector_pgvector:pg16",
        ),
        "emqx": (
            "docker.io/emqx/emqx:5.5.0-elixir",
            MIRROR + "emqx_emqx:5.5.0-elixir-amd64",
        ),
        "jaeger": (
            "docker.io/jaegertracing/all-in-one:1.76.0",
            MIRROR + "jaegertracing_all-in-one:1.76.0",
        ),
        "redis": (
            "docker.io/bitnamilegacy/redis-cluster:7.0.14",
            "registry.cn-shenzhen.aliyuncs.com/wl4g-k8s/bitnami_redis-cluster:7.0.14",
        ),
        "ldap": ("docker.io/glauth/glauth:v2.5.0", MIRROR + "glauth:v2.5.0"),
        "envoy": (
            "docker.io/envoyproxy/envoy:distroless-v1.36.4",
            MIRROR + "envoyproxy_envoy:distroless-v1.36.4",
        ),
        "sonarqube": (
            f"docker.io/library/sonarqube:{SONARQUBE_VERSION}",
            MIRROR + f"sonarqube:{SONARQUBE_VERSION}-amd64",
        ),
        "sonarqube-mcp": (
            "ghcr.io/flowgent-labs/sonarqube-mcp:v0.6.0",
            MIRROR + "ghcr.io_flowgent-labs_sonarqube-mcp:v0.6.0",
        ),
        "sonar-scanner": (
            "docker.io/sonarsource/sonar-scanner-cli:12.4.0.4321_8.4.0",
            MIRROR + "sonarsource_sonar-scanner-cli:12.4.0.4321_8.4.0",
        ),
        "helper": (
            "docker.io/rancher/mirrored-library-busybox:1.37.0",
            MIRROR + "rancher_mirrored-library-busybox:1.37.0",
        ),
        "authguard": (
            "ghcr.io/wl4g/authguard:1.0.1",
            MIRROR + "ghcr.io_wl4g_authguard:1.0.1",
        ),
        "authguard-web": (
            "ghcr.io/wl4g/authguard-web:1.0.1",
            MIRROR + "ghcr.io_wl4g_authguard-web:1.0.1",
        ),
        "envoy-gateway": (
            "docker.io/envoyproxy/gateway:v1.9.0",
            MIRROR + "envoyproxy_gateway:v1.9.0",
        ),
        "ratelimit": (
            "docker.io/envoyproxy/ratelimit:17b1956c",
            MIRROR + "envoyproxy_ratelimit:17b1956c",
        ),
        "python": (
            "docker.io/library/python:3.12-slim-bookworm",
            MIRROR + "python:3.12-slim-bookworm",
        ),
    }

    # Source: https://hub.docker.com/r/anzaxyz/agave/tags (v3.1.14).
    MATRIX.update(
        {
            "golang": (
                "docker.io/library/golang:1.26-alpine",
                MIRROR + "golang:1.26-alpine",
            ),
            "alpine": ("docker.io/library/alpine:3.21", MIRROR + "alpine:3.21"),
            "node": ("docker.io/library/node:22-alpine", MIRROR + "node:22-alpine"),
            "nginx": (
                "docker.io/library/nginx:1.27-alpine",
                MIRROR + "nginx:1.27-alpine",
            ),
            "postgres": (
                "docker.io/library/postgres:16-alpine",
                MIRROR + "postgres:16-alpine",
            ),
            "postgres18": (
                "docker.io/library/postgres:18.3",
                MIRROR + "postgres:18.3",
            ),
            "solana": (
                "docker.io/anzaxyz/agave:v3.1.14",
                MIRROR + "anza_solana:3.1.14",
            ),
        }
    )
    MACOS_IMAGES = {
        "solana": ("solana:3.1.14-arm64", MIRROR + "anza_solana:3.1.14-arm64")
    }
    NATIVE_ARM_IMAGES = {
        "sonarqube": (
            MATRIX["sonarqube"][0],
            MIRROR + f"sonarqube:{SONARQUBE_VERSION}-arm64",
        )
    }
    LOCAL_IMAGES = {
        "flowgent": "localhost/flowgent:latest",
        "flowgent-web": "localhost/flowgent-web:latest",
        "authguard-local": "localhost/e2e-flowgent-authguard:1.0.1",
        "authguard-web-local": "localhost/e2e-flowgent-authguard-web:1.0.1",
        "autotest-github": "localhost/e2e-autotest-github:dev",
        "autotest-sonarqube-mcp": "localhost/e2e-autotest-sonarqube-mcp:dev",
    }
    # k3s' installed provisioner requires this exact cache alias, regardless of
    # the registry from which its bytes were acquired.
    K3S_HELPER_REFERENCE = "docker.io/rancher/mirrored-library-busybox:1.37.0"

    @staticmethod
    def in_gfw() -> bool:
        def boolean(value):
            value = value.strip().lower()
            if value not in {
                "true",
                "false",
                "1",
                "0",
                "yes",
                "no",
                "in_gfw",
                "non_gfw",
            }:
                raise ValueError("IN_GFW/NON_GFW must contain a Boolean or region name")
            return value in {"true", "1", "yes", "in_gfw"}

        if "IN_GFW" in os.environ:
            return boolean(os.environ["IN_GFW"])
        if "NON_GFW" in os.environ:
            value = os.environ["NON_GFW"].strip().lower()
            return (
                value == "in_gfw"
                if value in {"in_gfw", "non_gfw"}
                else not boolean(value)
            )
        return boolean(os.getenv("IN_CN_GFW", "true"))

    @classmethod
    def get(cls, name: str) -> str:
        override = os.getenv("FLOWGENT_E2E_IMAGE_" + name.upper().replace("-", "_"))
        if override:
            if (
                name == "solana"
                and sys.platform == "darwin"
                and override not in cls.MACOS_IMAGES["solana"]
            ):
                raise ValueError(
                    "macOS Solana requires the pinned 3.1.14-arm64 image or its local alias"
                )
            return override
        if name in cls.LOCAL_IMAGES:
            return cls.LOCAL_IMAGES[name]
        return cls.definition_pair(name)[int(cls.in_gfw())]

    @classmethod
    def definition_pair(cls, name: str) -> tuple[str, str]:
        if (
            name in cls.NATIVE_ARM_IMAGES
            and os.getenv("E2E_ENGINE_PLATFORM") == "linux/arm64"
        ):
            return cls.NATIVE_ARM_IMAGES[name]
        pair = cls.MACOS_IMAGES.get(name) if sys.platform == "darwin" else None
        return pair or cls.MATRIX[name]

    @classmethod
    def key_for(cls, reference: str) -> str | None:
        if reference in cls.MATRIX or reference in cls.LOCAL_IMAGES:
            return reference
        for name, pair in cls.MATRIX.items():
            if (
                reference in pair
                or reference in cls.MACOS_IMAGES.get(name, ())
                or reference in cls.NATIVE_ARM_IMAGES.get(name, ())
                or reference == cls.get(name)
            ):
                return name
        for name, image in cls.LOCAL_IMAGES.items():
            if reference in {image, cls.get(name)}:
                return name
        return None

    @classmethod
    def platform(cls, reference: str = "") -> str:
        requested = os.getenv("FLOWGENT_E2E_PLATFORM", "linux/amd64")
        if requested != "linux/amd64":
            raise ValueError(
                "E2E workload images use linux/amd64; macOS Solana is selected automatically"
            )
        name = cls.key_for(reference) if reference else None
        native_arm = (
            name in cls.NATIVE_ARM_IMAGES
            and os.getenv("E2E_ENGINE_PLATFORM") == "linux/arm64"
        )
        return (
            "linux/arm64"
            if native_arm or (sys.platform == "darwin" and name == "solana")
            else "linux/amd64"
        )

    @classmethod
    def resolve(cls, image: str) -> str:
        name = cls.key_for(image)
        return cls.get(name) if name else image

    @classmethod
    def _substitutions(cls) -> dict[str, str]:
        result = {}
        for name, pair in cls.MATRIX.items():
            for reference in (
                *pair,
                *cls.MACOS_IMAGES.get(name, ()),
                *cls.NATIVE_ARM_IMAGES.get(name, ()),
            ):
                result[reference] = cls.get(name)
        for name, reference in cls.LOCAL_IMAGES.items():
            result[reference] = cls.get(name)
        return result

    @classmethod
    def rewrite(cls, value):
        if isinstance(value, str):
            substitutions = cls._substitutions()
            # Match complete references. A tag such as :18.3 must not rewrite
            # the prefix of :18.3-vmware-photon-5.0 or a different repository.
            pattern = (
                r"(?<![\w./:@-])(?:"
                + "|".join(
                    re.escape(key)
                    for key in sorted(substitutions, key=len, reverse=True)
                )
                + r")(?![\w./:@-])"
            )
            return re.sub(pattern, lambda match: substitutions[match.group()], value)
        if isinstance(value, list):
            return [cls.rewrite(item) for item in value]
        if isinstance(value, dict):
            return {key: cls.rewrite(item) for key, item in value.items()}
        return value

    @classmethod
    def references(cls, document) -> set[str]:
        """Collect Pod images and known references embedded in rendered configuration."""
        references = set()
        if isinstance(document, dict):
            for key, value in document.items():
                if key == "image" and isinstance(value, str) and value:
                    references.add(cls.resolve(value))
                references.update(cls.references(value))
        elif isinstance(document, list):
            for value in document:
                references.update(cls.references(value))
        elif isinstance(document, str):
            for reference, selected in cls._substitutions().items():
                if re.search(
                    r"(?<![\w./:@-])" + re.escape(reference) + r"(?![\w./:@-])",
                    document,
                ):
                    references.add(selected)
        return references

    @classmethod
    def render_stdin(cls) -> None:
        """Helm post-renderer: keep stdout exclusively for Kubernetes manifests."""
        import contextlib

        import yaml

        from .kubernetes import E2EKubernetesDeployer

        documents = [
            cls.rewrite(doc) for doc in yaml.safe_load_all(sys.stdin.read()) if doc
        ]
        if os.getenv("FLOWGENT_E2E_PRELOAD_IMAGES") == "1":
            with contextlib.redirect_stdout(sys.stderr):
                E2EKubernetesDeployer.prepare_images(documents)
        sys.stdout.write(yaml.safe_dump_all(documents, sort_keys=False))

    @classmethod
    def environment(cls) -> dict[str, str]:
        result = {"DOCKER_DEFAULT_PLATFORM": cls.platform()}
        for name in (*cls.MATRIX, *cls.LOCAL_IMAGES):
            suffix = name.upper().replace("-", "_")
            result["E2E_IMAGE_" + suffix] = cls.get(name)
            result["E2E_PLATFORM_" + suffix] = cls.platform(name)
        return result

    @classmethod
    def build_environment(cls, *, container: bool = True) -> dict[str, str]:
        return {
            "DOCKER_DEFAULT_PLATFORM": cls.platform(),
            "IN_CN_GFW": str(cls.in_gfw()).lower(),
            "HTTPS_PROXY": (
                os.getenv("FLOWGENT_E2E_BUILD_PROXY", os.getenv("HTTPS_PROXY", ""))
                if container
                else os.getenv("HTTPS_PROXY", "")
            ),
            "CORE_BUILD_TARGET": os.getenv("CORE_BUILD_TARGET", "runtime-prebuilt"),
            "CORE_GO_IMAGE": cls.get("golang"),
            "CORE_RUNTIME_IMAGE": cls.get("alpine"),
            "WEB_NODE_IMAGE": cls.get("node"),
            "WEB_NGINX_IMAGE": cls.get("nginx"),
            "CORE_IMAGE": cls.get("flowgent"),
            "CORE_LOCAL_IMAGE": cls.get("flowgent"),
            "WEB_IMAGE": cls.get("flowgent-web"),
            "WEB_LOCAL_IMAGE": cls.get("flowgent-web"),
        }

    @classmethod
    def runtime_environment(cls, engine_platform: str) -> dict[str, str]:
        translated = (
            engine_platform == "linux/arm64" and cls.platform() == "linux/amd64"
        )
        return {
            "E2E_ENGINE_PLATFORM": engine_platform,
            "E2E_EMQX_ERL_AFLAGS": os.getenv(
                "FLOWGENT_E2E_ERLANG_FLAGS", "+JPperf true" if translated else ""
            ),
        }

    @classmethod
    def parts(cls, name: str) -> tuple[str, str]:
        image = cls.get(name)
        if "@" in image:
            raise ValueError("This application chart requires a tagged image reference")
        repository, separator, tag = image.rpartition(":")
        return (repository, tag) if separator and "/" not in tag else (image, "latest")

    @classmethod
    def ensure_build_images(cls) -> None:
        names = ["alpine", "python", "nginx"]
        if os.getenv("CORE_BUILD_TARGET", "runtime-prebuilt") != "runtime-prebuilt":
            names.append("golang")
        if os.getenv("WEB_BUILD_TARGET", "runtime-dist") != "runtime-dist":
            names.append("node")
        for name in names:
            cls.ensure_local(cls.get(name))

    @classmethod
    def inspect(cls, image: str) -> dict | None:
        from .process import E2EProcessHelper

        formatting = "{{.Id}} {{.Os}}/{{.Architecture}}"
        result = E2EProcessHelper.run(
            (
                "docker",
                "image",
                "inspect",
                "--platform",
                cls.platform(image),
                "--format",
                formatting,
                image,
            ),
            timeout_seconds=30,
            stream=False,
        )
        # Some Docker/containerd stores cannot select a platform on a tagged
        # single-platform manifest. Inspect its metadata and still validate the
        # returned architecture before deciding whether acquisition is needed.
        if not result.passed:
            result = E2EProcessHelper.run(
                ("docker", "image", "inspect", "--format", formatting, image),
                timeout_seconds=30,
                stream=False,
            )
        fields = result.output.strip().split()
        return (
            {"id": fields[0], "platform": fields[1]}
            if result.passed and len(fields) == 2
            else None
        )

    @classmethod
    def prepare_runtime(cls, image: str) -> bool:
        """Acquire the exact runtime architecture, without building intermediate images."""
        image = cls.resolve(image)
        metadata = cls.inspect(image)
        if metadata and metadata["platform"] == cls.platform(image):
            return True
        if (
            cls.in_gfw()
            and sys.platform != "win32"
            and (Path.home() / "mirror-pull.sh").is_file()
        ):
            try:
                cls.ensure_local(image)
                return True
            except RuntimeError:
                # Runtime pulls also work on Docker-only hosts where the local
                # jump-host script has no supported Kubernetes transfer target.
                pass
        cls._pull(image, jump_host=False)
        return True

    @classmethod
    def ensure_local(cls, image: str) -> None:
        metadata = cls.inspect(image)
        if metadata and metadata["platform"] == cls.platform(image):
            return
        cls._pull(image, jump_host=cls.in_gfw() and sys.platform != "win32")

    @classmethod
    def _pull(cls, image: str, *, jump_host: bool) -> None:
        from .process import E2EProcessHelper

        if cls.key_for(image) in cls.LOCAL_IMAGES:
            raise RuntimeError(
                f"Local image {image} must be built for {cls.platform(image)} through the root Makefile"
            )
        source = (
            cls.MACOS_IMAGES["solana"][1]
            if image == cls.MACOS_IMAGES["solana"][0]
            else image
        )
        platform = cls.platform(image)
        environment = {"MIRROR_PLATFORM": platform, "DOCKER_DEFAULT_PLATFORM": platform}
        if jump_host:
            mirror = Path.home() / "mirror-pull.sh"
            if not mirror.is_file():
                raise RuntimeError(
                    f"Required jump-host image loader is missing: {mirror}"
                )
            key = cls.key_for(image)
            definitions = (
                *cls.MATRIX.get(key, ()),
                *cls.MACOS_IMAGES.get(key, ()),
                *cls.NATIVE_ARM_IMAGES.get(key, ()),
            )
            if key and image in definitions:
                destination = cls.definition_pair(key)
                registry, namespace, leaf = destination[1].split("/", 2)
                repository, tag = leaf.rsplit(":", 1)
                # The custom ARM Solana artifact already lives in the given mirror.
                source = (
                    destination[1]
                    if key == "solana" and sys.platform == "darwin"
                    else cls.MATRIX[key][0]
                )
                environment.update(
                    {"MIRROR_REG": registry, "MIRROR_NS": namespace, "MIRROR_TAG": tag}
                )
                command = (str(mirror), source, repository)
            elif image.startswith("registry.cn-shenzhen.aliyuncs.com/"):
                registry, namespace, leaf = image.split("/", 2)
                repository, tag = leaf.rsplit(":", 1)
                environment.update(
                    {"MIRROR_REG": registry, "MIRROR_NS": namespace, "MIRROR_TAG": tag}
                )
                command = (str(mirror), image, repository)
            else:
                command = (str(mirror), source)
        else:
            command = ("docker", "pull", "--platform", platform, source)
        result = E2EProcessHelper.run(
            command,
            environment=environment,
            timeout_seconds=1200,
        )
        if not result.passed:
            raise RuntimeError(f"Image acquisition failed for {source} ({platform})")
        if source != image:
            tagged = E2EProcessHelper.run(
                ("docker", "tag", source, image), timeout_seconds=30
            )
            if not tagged.passed:
                raise RuntimeError(
                    f"Cannot create the required local image alias {image}"
                )
        metadata = cls.inspect(image)
        if not metadata or metadata["platform"] != platform:
            raise RuntimeError(f"Image loader did not retain {image} for {platform}")


if __name__ == "__main__":
    E2EImagesDefinitions.render_stdin()
