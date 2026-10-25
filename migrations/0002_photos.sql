-- +goose Up
-- Metadata foto (spec §7.2). Byte foto ada di SSD, dirujuk lewat storage_key.
-- FK submission_id → submissions ditambahkan di 0003 dan owner_id → users di migrasi auth,
-- karena tabel tujuannya belum ada pada titik ini.
CREATE TABLE photos (
    id            uuid        PRIMARY KEY,
    submission_id uuid        NULL,
    owner_id      uuid        NULL,
    position      smallint    NOT NULL DEFAULT 0 CHECK (position >= 0),
    storage_key   text        NOT NULL UNIQUE,
    content_type  text        NOT NULL CHECK (content_type IN ('image/webp', 'image/jpeg')),
    size_bytes    integer     NOT NULL CHECK (size_bytes > 0),
    width         integer     NOT NULL CHECK (width > 0 AND width <= 2048),
    height        integer     NOT NULL CHECK (height > 0 AND height <= 2048),
    uploaded_at   timestamptz NULL,
    sha256        bytea       NULL CHECK (sha256 IS NULL OR octet_length(sha256) = 32),
    status        text        NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'attached', 'rejected', 'deleted')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- foto dianggap ter-upload hanya jika hash-nya tercatat, dan sebaliknya
    CONSTRAINT photos_uploaded_has_sha CHECK ((uploaded_at IS NULL) = (sha256 IS NULL)),
    -- foto yang sudah dipakai harus punya submission
    CONSTRAINT photos_attached_has_submission CHECK (status <> 'attached' OR submission_id IS NOT NULL)
);

CREATE INDEX photos_status_created_idx ON photos (status, created_at);
CREATE INDEX photos_submission_idx ON photos (submission_id);

-- +goose Down
DROP TABLE photos;
