-- +goose Up
-- +goose StatementBegin
CREATE TABLE active_session (
    card_id      TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    phase        TEXT NOT NULL,
    agent        TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (card_id) REFERENCES work_cards(id) ON DELETE CASCADE
);

CREATE INDEX idx_active_session_phase ON active_session(phase);
CREATE INDEX idx_active_session_agent ON active_session(agent);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS active_session;
-- +goose StatementEnd
