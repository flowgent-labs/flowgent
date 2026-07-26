-- Alter knowledge_entries.content from JSONB to TEXT (to match SQLite schema).
-- The content is always stored and queried as plain text; the JSONB type was
-- an oversight that causes INSERT failures when the Go entity passes a plain
-- Go string to the PostgreSQL driver.
ALTER TABLE knowledge_entries ALTER COLUMN content TYPE TEXT USING content::text;
