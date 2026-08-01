-- +goose Up
-- +goose StatementBegin
ALTER TABLE work_cards ADD COLUMN coding_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE work_cards ADD COLUMN reviewer_mode TEXT NOT NULL DEFAULT 'same';
ALTER TABLE work_cards ADD COLUMN reviewer_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE work_cards ADD COLUMN testing_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE work_cards ADD COLUMN redo_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE work_cards ADD COLUMN latest_redo_summary TEXT NOT NULL DEFAULT '';

CREATE TABLE work_card_redo_cycles (
    id           TEXT PRIMARY KEY,
    card_id      TEXT NOT NULL REFERENCES work_cards(id) ON DELETE CASCADE,
    cycle_number INTEGER NOT NULL,
    source       TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    completed_at INTEGER,
    UNIQUE (card_id, cycle_number)
);

CREATE INDEX idx_work_card_redo_cycles_card ON work_card_redo_cycles(card_id, cycle_number);

CREATE TABLE work_card_findings (
    id             TEXT PRIMARY KEY,
    cycle_id       TEXT NOT NULL REFERENCES work_card_redo_cycles(id) ON DELETE CASCADE,
    sequence       INTEGER NOT NULL,
    severity       TEXT NOT NULL,
    title          TEXT NOT NULL,
    details        TEXT NOT NULL DEFAULT '',
    command        TEXT NOT NULL DEFAULT '',
    error_output   TEXT NOT NULL DEFAULT '',
    file_refs_json TEXT NOT NULL DEFAULT '[]',
    status         TEXT NOT NULL DEFAULT 'pending',
    attempt_count  INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    UNIQUE (cycle_id, sequence)
);

CREATE INDEX idx_work_card_findings_cycle ON work_card_findings(cycle_id, sequence);

CREATE TABLE work_card_attempts (
    id              TEXT PRIMARY KEY,
    finding_id      TEXT NOT NULL REFERENCES work_card_findings(id) ON DELETE CASCADE,
    attempt_number  INTEGER NOT NULL,
    agent           TEXT NOT NULL,
    started_at      INTEGER NOT NULL,
    finished_at     INTEGER,
    result          TEXT NOT NULL DEFAULT '',
    output          TEXT NOT NULL DEFAULT '',
    validation_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE (finding_id, attempt_number)
);

CREATE INDEX idx_work_card_attempts_finding ON work_card_attempts(finding_id, attempt_number);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS work_card_attempts;
DROP TABLE IF EXISTS work_card_findings;
DROP TABLE IF EXISTS work_card_redo_cycles;
-- Note: SQLite table column drops require table recreation; for goose down we drop the new tables.
-- +goose StatementEnd
