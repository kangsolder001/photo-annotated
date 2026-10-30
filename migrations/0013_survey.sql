-- +goose Up
-- Pendataan baseline (spec-pendataan §6): submission diperluas dengan jawaban instrumen. Kolom boleh NULL
-- agar migrasi aman untuk baris lama; aplikasi selalu mengisinya untuk pendataan baru.
ALTER TABLE submissions
    ADD COLUMN form_version integer NULL CHECK (form_version IS NULL OR form_version >= 1),
    ADD COLUMN answers      jsonb   NULL CHECK (answers IS NULL OR jsonb_typeof(answers) = 'object'),
    ADD COLUMN consent      boolean GENERATED ALWAYS AS ((answers->>'a_consent') = 'yes') STORED,
    ADD COLUMN survey_date  date    NULL,  -- salinan answers.a_date untuk filter/indeks
    ADD COLUMN rw           text    NULL;  -- salinan answers.a_rw
CREATE INDEX submissions_survey_date_idx ON submissions (survey_date);

-- label kotak bawaan, hanya bila tabel labels masih kosong (instance baru)
INSERT INTO labels (name, color, sort_order)
SELECT v.name, v.color, v.sort_order
  FROM (VALUES ('Organik', '#2e7d32', 1), ('Anorganik', '#1565c0', 2), ('Residu', '#6d4c41', 3), ('B3', '#c62828', 4))
       AS v(name, color, sort_order)
 WHERE NOT EXISTS (SELECT 1 FROM labels);

-- +goose Down
-- Down tidak boleh menghapus data: gagal bila sudah ada jawaban pendataan. Label bawaan dibiarkan
-- (bisa sudah dipakai anotasi).
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM submissions WHERE answers IS NOT NULL) THEN
        RAISE EXCEPTION 'migrasi 0013 tidak bisa dibatalkan: sudah ada submission dengan jawaban pendataan; hapus/ekspor dulu secara manual';
    END IF;
END
$$;
-- +goose StatementEnd
DROP INDEX submissions_survey_date_idx;
ALTER TABLE submissions
    DROP COLUMN rw,
    DROP COLUMN survey_date,
    DROP COLUMN consent,
    DROP COLUMN answers,
    DROP COLUMN form_version;
