-- name: UpsertRSVP :exec
INSERT INTO rsvps (meeting_number, name, email, newsletter_opt_in, responses)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(meeting_number, email) DO UPDATE SET
    name = excluded.name,
    newsletter_opt_in = excluded.newsletter_opt_in,
    responses = excluded.responses;

-- name: RSVPExists :one
SELECT COUNT(*) FROM rsvps WHERE meeting_number = ? AND email = ?;

-- name: RSVPsByMeeting :many
SELECT id, meeting_number, name, email, newsletter_opt_in, responses, created_at
FROM rsvps
WHERE meeting_number = ?
ORDER BY created_at ASC;
