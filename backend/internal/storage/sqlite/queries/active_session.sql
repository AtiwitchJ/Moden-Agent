-- name: InsertActiveSession :exec
INSERT INTO active_session (card_id, session_id, phase, agent, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetActiveSession :one
SELECT * FROM active_session WHERE card_id = ?;

-- name: ListActiveSessionsByPhase :many
SELECT * FROM active_session WHERE phase = ?;

-- name: ListActiveSessionsByAgent :many
SELECT * FROM active_session WHERE agent = ?;

-- name: DeleteActiveSession :exec
DELETE FROM active_session WHERE card_id = ?;
