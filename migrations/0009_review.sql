-- +goose Up
-- Review submission oleh admin (spec-labels-admin §5.7, §6.4). Submission lama otomatis pending.
-- Submission accepted tidak ikut retensi 90 hari.
ALTER TABLE submissions
    ADD COLUMN review_status text NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'accepted', 'rejected')),
    ADD COLUMN review_reason text NULL CHECK (review_reason IS NULL OR char_length(review_reason) BETWEEN 1 AND 500),
    ADD COLUMN reviewed_at   timestamptz NULL,
    -- reviewed_at kosong ⇔ pending; alasan wajib untuk rejected dan kosong untuk pending
    ADD CONSTRAINT submissions_review_consistent CHECK (
        ((review_status = 'pending') = (reviewed_at IS NULL))
        AND (review_status <> 'rejected' OR review_reason IS NOT NULL)
        AND (review_status <> 'pending' OR review_reason IS NULL)
    );

-- antrean review, filter status, dan query retensi
CREATE INDEX submissions_review_created_idx ON submissions (review_status, created_at);

-- +goose Down
DROP INDEX submissions_review_created_idx;
ALTER TABLE submissions
    DROP CONSTRAINT submissions_review_consistent,
    DROP COLUMN reviewed_at,
    DROP COLUMN review_reason,
    DROP COLUMN review_status;
