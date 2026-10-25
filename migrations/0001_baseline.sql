-- +goose Up
-- Baseline kosong. Skema dibuat bertahap oleh migrasi berikutnya (photos, submissions, auth, ...)
-- supaya setiap vertical slice membawa skemanya sendiri.
SELECT 1;

-- +goose Down
SELECT 1;
