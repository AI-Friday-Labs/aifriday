-- name: InsertSlackLink :exec
INSERT OR IGNORE INTO slack_links (
    url, channel_id, channel_name, user_id, user_name, message_ts, message_text
)
VALUES
    (?, ?, ?, ?, ?, ?, ?);

-- name: SlackLinksSince :many
SELECT
    id, url, channel_id, channel_name, user_id, user_name, message_ts, message_text, created_at
FROM
    slack_links
WHERE
    created_at >= ?
ORDER BY
    created_at DESC;

-- name: SlackLinkCount :one
SELECT
    COUNT(*)
FROM
    slack_links
WHERE
    created_at >= ?;
