-- +goose Up
-- Kelompok mahasiswa (spec-pendataan §5.5, §6): daftar teks bebas yang dikelola admin, dipilih saat
-- registrasi. Kolom users.group_id menyusul di 0012.

CREATE TABLE groups (
    id          integer     GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    archived_at timestamptz NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- unik tanpa membedakan huruf besar-kecil, termasuk terhadap kelompok yang diarsipkan
CREATE UNIQUE INDEX groups_name_lower_idx ON groups (lower(name));

-- +goose Down
DROP TABLE groups;
