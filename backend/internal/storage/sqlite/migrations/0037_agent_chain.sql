-- +goose Up
ALTER TABLE work_cards ADD COLUMN reviewer_agents_json TEXT;
ALTER TABLE work_cards ADD COLUMN testing_agents_json TEXT;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE work_cards DROP COLUMN reviewer_agents_json;
ALTER TABLE work_cards DROP COLUMN testing_agents_json;
-- +goose StatementEnd
