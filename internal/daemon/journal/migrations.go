package journal

var migrations = []string{
	// 0: Initial journal_files and journal_entries tables and indexes
	`
CREATE TABLE IF NOT EXISTS journal_files (
    session_id TEXT NOT NULL,
    path       TEXT NOT NULL,
    baseline   TEXT NOT NULL DEFAULT '',
    binary     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, path)
);

CREATE TABLE IF NOT EXISTS journal_entries (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    path         TEXT NOT NULL,
    turn_id      TEXT NOT NULL DEFAULT '',
    message_id   TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    source       TEXT NOT NULL,
    tool         TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL,
    before_hash  TEXT NOT NULL DEFAULT '',
    after_hash   TEXT NOT NULL DEFAULT '',
    additions    INTEGER NOT NULL DEFAULT 0,
    deletions    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_journal_entries_session
    ON journal_entries(session_id, id ASC);
CREATE INDEX IF NOT EXISTS idx_journal_entries_session_path
    ON journal_entries(session_id, path, id ASC);
`,
}
