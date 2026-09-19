"""Scenario 14 — real LDAP, GitHub OAuth, Envoy, and AuthGuard E2E."""

from common import config
from deploy.authguard_e2e import bootstrap_and_verify


def run():
    bootstrap_and_verify(config.SYSTEM_NAMESPACE)
