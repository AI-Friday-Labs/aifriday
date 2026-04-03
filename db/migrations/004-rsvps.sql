-- 004-rsvps.sql
CREATE TABLE IF NOT EXISTS rsvps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    meeting_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    newsletter_opt_in BOOLEAN NOT NULL DEFAULT 0,
    responses TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(meeting_number, email)
);

CREATE INDEX IF NOT EXISTS idx_rsvps_meeting ON rsvps(meeting_number);

INSERT INTO migrations (migration_number) VALUES (4);
