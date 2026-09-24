"""
Flowgent E2E — shared path constants.
"""

import os

E2E_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
USE_CASE_DIR = os.path.dirname(E2E_DIR)
PROJECT_ROOT = os.path.abspath(os.path.join(USE_CASE_DIR, "..", ".."))

HELM_CHART = os.path.join(PROJECT_ROOT, "deploy", "helm", "flowgent")
SONAR_COMPOSE = os.path.join(PROJECT_ROOT, "deploy", "docker", "sonarqube", "docker-compose.yml")
CONFIG_DIR = os.path.join(USE_CASE_DIR, "config")
CONSOLE_BIN = os.path.join(PROJECT_ROOT, "bin", "flowgent-core")
CONSOLE_CFG = os.path.join(PROJECT_ROOT, "etc", "flowgent.yaml")
