-- Slack links table
--
-- Stores URLs shared in Slack channels for tracking and reporting.

CREATE TABLE IF NOT EXISTS slack_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    channel_name TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL,
    user_name TEXT NOT NULL DEFAULT '',
    message_ts TEXT NOT NULL,
    message_text TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (url, channel_id, message_ts)
);

CREATE INDEX IF NOT EXISTS idx_slack_links_created_at ON slack_links (created_at);

-- Record execution of this migration
INSERT
OR IGNORE INTO migrations (migration_number, migration_name)
VALUES
    (002, '002-slack-links');
