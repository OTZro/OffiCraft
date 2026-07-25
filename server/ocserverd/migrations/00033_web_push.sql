-- +goose Up
CREATE TABLE push_subscription (
    endpoint TEXT PRIMARY KEY,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    expiration_time REAL,
    created_ts REAL NOT NULL,
    updated_ts REAL NOT NULL
);

-- +goose Down
DROP TABLE push_subscription;
