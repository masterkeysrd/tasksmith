package timeline

var migrations = []string{
	// 0: Initial timeline table and session index
	`
CREATE TABLE IF NOT EXISTS timeline (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_timeline_session_id ON timeline(session_id, id ASC);
`,
}
