-- name: InsertWorkCard :exec
INSERT INTO work_cards (
  id, project_id, board_id, title, notes, priority, labels_json, status,
  scheduled_at, ready_at, position, target_path, repo_name, agent,
  coding_agent, reviewer_mode, reviewer_agent, testing_agent, redo_count, latest_redo_summary,
  session_id, waiting_for_input, paused_retarget, goal_version, superseded_by_card_id,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkCard :one
SELECT * FROM work_cards WHERE id = ?;

-- name: ListWorkCardsByProject :many
SELECT * FROM work_cards
WHERE project_id = ? AND board_id = ?
ORDER BY status, position, created_at;

-- name: ListAllWorkCards :many
SELECT * FROM work_cards
ORDER BY status, position, created_at;

-- name: UpdateWorkCard :exec
UPDATE work_cards SET
  title = ?, notes = ?, priority = ?, labels_json = ?, status = ?,
  scheduled_at = ?, ready_at = ?, position = ?, target_path = ?, repo_name = ?,
  agent = ?, coding_agent = ?, reviewer_mode = ?, reviewer_agent = ?, testing_agent = ?,
  redo_count = ?, latest_redo_summary = ?,
  session_id = ?, waiting_for_input = ?, paused_retarget = ?,
  goal_version = ?, superseded_by_card_id = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteWorkCard :exec
DELETE FROM work_cards WHERE id = ?;

-- name: CountRunningCards :one
SELECT COUNT(*) FROM work_cards
WHERE project_id = ? AND status = 'running';

-- name: ClaimReadyWorkCard :execrows
UPDATE work_cards
SET status = 'running', session_id = '', updated_at = sqlc.arg(updated_at)
WHERE work_cards.id = sqlc.arg(card_id)
  AND work_cards.project_id = sqlc.arg(project_id)
  AND work_cards.status = 'ready'
  AND work_cards.paused_retarget = 0
  AND (
    SELECT COUNT(*) FROM work_cards AS running_cards
    WHERE running_cards.project_id = sqlc.arg(project_id) AND running_cards.status = 'running'
  ) < CAST(sqlc.arg(wip_limit) AS INTEGER);

-- name: InsertWorkCardEvent :exec
INSERT INTO work_card_events (id, card_id, project_id, kind, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListWorkCardEventsByCard :many
SELECT * FROM work_card_events
WHERE card_id = ?
ORDER BY created_at, id;

-- name: InsertRedoCycle :exec
INSERT INTO work_card_redo_cycles (
  id, card_id, cycle_number, source, summary, created_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetLatestRedoCycle :one
SELECT * FROM work_card_redo_cycles
WHERE card_id = ?
ORDER BY cycle_number DESC
LIMIT 1;

-- name: ListRedoCyclesByCard :many
SELECT * FROM work_card_redo_cycles
WHERE card_id = ?
ORDER BY cycle_number ASC;

-- name: CompleteRedoCycle :exec
UPDATE work_card_redo_cycles
SET completed_at = ?
WHERE id = ?;

-- name: InsertRedoFinding :exec
INSERT INTO work_card_findings (
  id, cycle_id, sequence, severity, title, details, command, error_output, file_refs_json, status, attempt_count, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListRedoFindingsByCycle :many
SELECT * FROM work_card_findings
WHERE cycle_id = ?
ORDER BY sequence ASC;

-- name: UpdateRedoFindingStatus :exec
UPDATE work_card_findings
SET status = ?, attempt_count = attempt_count + ?, updated_at = ?
WHERE id = ?;

-- name: InsertRedoAttempt :exec
INSERT INTO work_card_attempts (
  id, finding_id, attempt_number, agent, started_at, finished_at, result, output, validation_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListRedoAttemptsByFinding :many
SELECT * FROM work_card_attempts
WHERE finding_id = ?
ORDER BY attempt_number ASC;
