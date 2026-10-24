-- +goose Up
-- Submission dan anotasi (spec §7.1, §7.3). owner_id → users ditambahkan di migrasi auth.
CREATE TABLE submissions (
    id                uuid        PRIMARY KEY,
    owner_id          uuid        NULL,
    client_request_id uuid        NOT NULL UNIQUE,   -- kunci idempotensi
    client_ip         inet        NULL,
    version           integer     NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX submissions_owner_idx   ON submissions (owner_id);
CREATE INDEX submissions_created_idx ON submissions (created_at);

-- Foto yang dipakai submission ikut terhapus bila submission dihapus (retensi). File di SSD
-- dihapus lebih dulu oleh cleanup sebelum baris dihapus.
ALTER TABLE photos
    ADD CONSTRAINT photos_submission_fk
    FOREIGN KEY (submission_id) REFERENCES submissions (id) ON DELETE CASCADE;

-- Koordinat ternormalisasi 0–1 relatif terhadap foto hasil proses (spec §6.3). double precision
-- (bukan real) agar 0.12 kembali persis sama saat dibaca/diekspor. Toleransi 1e-6 pada
-- x+w dan y+h sama dengan domain.BBoxEpsilon.
CREATE TABLE annotations (
    id         bigserial        PRIMARY KEY,
    photo_id   uuid             NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    text       text             NOT NULL CHECK (char_length(text) BETWEEN 1 AND 1000),
    x          double precision NOT NULL CHECK (x >= 0 AND x < 1),
    y          double precision NOT NULL CHECK (y >= 0 AND y < 1),
    w          double precision NOT NULL CHECK (w >= 0.005 AND w <= 1),
    h          double precision NOT NULL CHECK (h >= 0.005 AND h <= 1),
    created_at timestamptz      NOT NULL DEFAULT now(),
    CONSTRAINT annotations_inside_photo CHECK (x + w <= 1.000001 AND y + h <= 1.000001)
);

CREATE INDEX annotations_photo_idx ON annotations (photo_id);

-- +goose Down
DROP TABLE annotations;
ALTER TABLE photos DROP CONSTRAINT photos_submission_fk;
DROP TABLE submissions;
