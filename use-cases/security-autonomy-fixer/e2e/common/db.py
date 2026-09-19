"""
Flowgent E2E — shared database utilities.
"""

from . import config


def pg_connect():
    """Connect to the Flowgent PostgreSQL database.

    Tries the configured DSN first, then falls back to port 5433 (common
    when PG is port-forwarded or running on an alternate port).
    Returns a psycopg2 connection with autocommit enabled, or None.
    """
    try:
        import psycopg2
    except ImportError:
        print("  SKIP: psycopg2 not installed")
        return None
    dsn = config.pg_dsn()
    try:
        conn = psycopg2.connect(dsn)
        conn.autocommit = True
        return conn
    except Exception:
        try:
            alt = dsn.replace("port=5432", "port=5433")
            conn = psycopg2.connect(alt)
            conn.autocommit = True
            return conn
        except Exception:
            return None
