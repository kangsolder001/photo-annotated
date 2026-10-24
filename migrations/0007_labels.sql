-- +goose Up
-- Label terkontrol (spec-labels-admin §4, §6.1–6.2). Anotasi baru menyimpan label_id; teks bebas lama
-- dipertahankan apa adanya sampai habis oleh retensi.

CREATE TABLE labels (
    -- tidak pernah dipakai ulang: stabil sebagai indeks kelas saat export
    id          integer     GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    description text        NULL CHECK (description IS NULL OR char_length(description) BETWEEN 1 AND 200),
    color       text        NOT NULL CHECK (color ~ '^#[0-9a-f]{6}$'),
    sort_order  integer     NOT NULL DEFAULT 0,
    archived_at timestamptz NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- unik tanpa membedakan huruf besar-kecil, termasuk terhadap label yang diarsipkan
CREATE UNIQUE INDEX labels_name_lower_idx ON labels (lower(name));
CREATE INDEX labels_active_order_idx ON labels (sort_order) WHERE archived_at IS NULL;

ALTER TABLE annotations
    ADD COLUMN label_id integer NULL REFERENCES labels (id) ON DELETE RESTRICT,
    ALTER COLUMN text DROP NOT NULL,
    ADD CONSTRAINT annotations_label_or_text CHECK (label_id IS NOT NULL OR text IS NOT NULL);

-- usage_count, filter label_id, dan pengecekan hapus label
CREATE INDEX annotations_label_idx ON annotations (label_id);

-- +goose Down
-- Down tidak boleh menghapus data: gagal bila sudah ada anotasi berlabel.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM annotations WHERE label_id IS NOT NULL) THEN
        RAISE EXCEPTION 'migrasi 0007 tidak bisa dibatalkan: sudah ada anotasi yang memakai label (label_id); hapus/ekspor dulu secara manual';
    END IF;
END
$$;
-- +goose StatementEnd
DROP INDEX annotations_label_idx;
ALTER TABLE annotations
    DROP CONSTRAINT annotations_label_or_text,
    ALTER COLUMN text SET NOT NULL,
    DROP COLUMN label_id;
DROP TABLE labels;
