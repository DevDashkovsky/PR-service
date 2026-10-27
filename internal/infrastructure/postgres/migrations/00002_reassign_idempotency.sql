-- +goose Up
CREATE TABLE pr_reassign_idempotency (
    idempotency_key VARCHAR(128) COLLATE "C" PRIMARY KEY,
    pull_request_id VARCHAR(255) NOT NULL,
    old_user_id VARCHAR(255) NOT NULL,
    response JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE pr_reassign_idempotency;
