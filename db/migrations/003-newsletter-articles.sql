-- Newsletter articles extracted from email newsletters
CREATE TABLE IF NOT EXISTS newsletter_articles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    newsletter TEXT NOT NULL,  -- e.g. 'The Rundown AI', 'TLDR AI'
    email_subject TEXT NOT NULL DEFAULT '',
    email_date TEXT NOT NULL DEFAULT '',  -- RFC3339 or YYYY-MM-DD HH:MM:SS
    email_file TEXT NOT NULL DEFAULT '',   -- original .eml filename
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    UNIQUE(url, newsletter)  -- dedup same link across issues of same newsletter
);

CREATE INDEX IF NOT EXISTS idx_newsletter_articles_created ON newsletter_articles(created_at);
CREATE INDEX IF NOT EXISTS idx_newsletter_articles_newsletter ON newsletter_articles(newsletter);
