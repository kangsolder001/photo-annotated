-- +goose Up
-- Blokir annotator oleh admin (spec-labels-admin §5.8, §6.5). Data user tidak berubah saat diblokir.
ALTER TABLE users
    ADD COLUMN blocked_at     timestamptz NULL,
    ADD COLUMN blocked_reason text NULL CHECK (blocked_reason IS NULL OR char_length(blocked_reason) BETWEEN 1 AND 500),
    ADD CONSTRAINT users_blocked_has_reason CHECK ((blocked_at IS NULL) = (blocked_reason IS NULL));

-- +goose Down
ALTER TABLE users
    DROP CONSTRAINT users_blocked_has_reason,
    DROP COLUMN blocked_reason,
    DROP COLUMN blocked_at;
