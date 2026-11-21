"""PostgreSQL initialization and read evidence, independent of application tables."""

from __future__ import annotations

import re


class E2EPostgresChecker:
    @staticmethod
    def connect(dsn: str, *, required: bool = True):
        try:
            import psycopg2

            connection = psycopg2.connect(dsn, connect_timeout=10)
            connection.autocommit = True
            return connection
        except Exception:
            if required:
                raise
            return None

    @staticmethod
    def query(connection, statement: str, parameters: tuple = ()) -> list[tuple]:
        with connection.cursor() as cursor:
            cursor.execute(statement, parameters)
            return cursor.fetchall()


class E2EPostgresImporter:
    @staticmethod
    def execute(connection, statement: str, parameters: tuple = ()) -> None:
        with connection.cursor() as cursor:
            cursor.execute(statement, parameters)

    @staticmethod
    def reset_schema(connection, schema: str, owner: str) -> None:
        from psycopg2 import sql

        if not re.fullmatch(r"[a-z_][a-z0-9_]*", schema):
            raise ValueError(f"invalid isolated PostgreSQL schema: {schema!r}")
        with connection.cursor() as cursor:
            cursor.execute(
                sql.SQL("DROP SCHEMA IF EXISTS {} CASCADE").format(
                    sql.Identifier(schema)
                )
            )
            cursor.execute(
                sql.SQL("CREATE SCHEMA {} AUTHORIZATION {}").format(
                    sql.Identifier(schema), sql.Identifier(owner)
                )
            )
