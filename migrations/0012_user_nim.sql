-- +goose Up
-- NIM (unik) dan kelompok mahasiswa (spec-pendataan §5.4, §6). Boleh NULL di DB (mode none dan akun
-- lama), tetapi wajib untuk registrasi baru di mode otp.
ALTER TABLE users
    ADD COLUMN nim      text    NULL CHECK (nim ~ '^[A-Z0-9]{5,20}$'),
    ADD COLUMN group_id integer NULL REFERENCES groups (id) ON DELETE RESTRICT;
-- NIM hanya terisi saat akun aktif (diterapkan dari challenge saat OTP diverifikasi): indeks unik ini
-- menegakkan "satu NIM satu akun", termasuk saat dua pendaftar memverifikasi berbarengan.
CREATE UNIQUE INDEX users_nim_idx ON users (nim);
CREATE INDEX users_group_idx ON users (group_id);

-- Challenge registrasi membawa NIM + kelompok sampai OTP diverifikasi (pola sama dengan reg_name/email).
-- Tanpa FK ke groups: kelompok yang dihapus sebelum verifikasi ditolak saat diterapkan ke users.
ALTER TABLE otp_challenges
    ADD COLUMN reg_nim      text    NULL CHECK (reg_nim IS NULL OR reg_nim ~ '^[A-Z0-9]{5,20}$'),
    ADD COLUMN reg_group_id integer NULL,
    ADD CONSTRAINT otp_registration_nim CHECK (purpose = 'register' OR (reg_nim IS NULL AND reg_group_id IS NULL));

-- +goose Down
-- Down tidak boleh menghapus data: gagal bila sudah ada NIM terisi.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE nim IS NOT NULL) THEN
        RAISE EXCEPTION 'migrasi 0012 tidak bisa dibatalkan: sudah ada pengguna dengan NIM; hapus/ekspor dulu secara manual';
    END IF;
END
$$;
-- +goose StatementEnd
ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_registration_nim,
    DROP COLUMN reg_group_id,
    DROP COLUMN reg_nim;
DROP INDEX users_group_idx;
DROP INDEX users_nim_idx;
ALTER TABLE users
    DROP COLUMN group_id,
    DROP COLUMN nim;
