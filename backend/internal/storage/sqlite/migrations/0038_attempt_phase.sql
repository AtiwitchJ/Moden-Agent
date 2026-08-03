-- +goose Up
-- +goose StatementBegin
ALTER TABLE work_card_attempts ADD COLUMN phase TEXT;
ALTER TABLE work_card_attempts ADD COLUMN failure_reason TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE work_card_attempts DROP COLUMN phase;
ALTER TABLE work_card_attempts DROP COLUMN failure_reason;
-- +goose StatementEnd
