"""Native pgvector extension initialization, shared by persistence consumers."""


class E2EPgvectorImporter:
    @staticmethod
    def initialize(connection) -> str:
        with connection.cursor() as cursor:
            cursor.execute("CREATE EXTENSION IF NOT EXISTS vector")
            cursor.execute("SELECT extversion FROM pg_extension WHERE extname='vector'")
            version = cursor.fetchone()[0]
            cursor.execute("SELECT '[1,2,3]'::vector <-> '[1,2,4]'::vector")
            if float(cursor.fetchone()[0]) != 1.0:
                raise AssertionError("native pgvector distance did not equal 1.0")
        return version
