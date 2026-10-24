-- +goose Up
-- Login dengan nomor HP + password; OTP hanya untuk verifikasi nomor saat registrasi dan reset password.

-- hash bcrypt; NULL = belum pernah membuat password (pengguna lama atau masih pending)
ALTER TABLE users ADD COLUMN password_hash text NULL
    CHECK (password_hash IS NULL OR (password_hash LIKE '$2%' AND char_length(password_hash) = 60));

-- Challenge lama (dibuat sebelum ada password) tidak punya data registrasi dan tidak akan memenuhi
-- constraint baru; challenge OTP berumur menit, jadi aman dibuang.
DELETE FROM otp_challenges;

-- Tujuan challenge. Data registrasi disimpan di challenge (bukan di baris users) dan baru ditulis ke
-- pengguna saat kode challenge itu diverifikasi: pendaftaran ulang nomor yang sama oleh orang lain
-- hanya mengganti challenge, tidak bisa menyisipkan password ke akun pemilik nomor.
ALTER TABLE otp_challenges
    ADD COLUMN purpose text NOT NULL DEFAULT 'register' CHECK (purpose IN ('register', 'reset')),
    ADD COLUMN reg_name text NULL CHECK (reg_name IS NULL OR char_length(reg_name) BETWEEN 1 AND 100),
    ADD COLUMN reg_email text NULL CHECK (reg_email IS NULL OR char_length(reg_email) BETWEEN 3 AND 254),
    ADD COLUMN reg_password_hash text NULL CHECK (reg_password_hash IS NULL OR char_length(reg_password_hash) = 60),
    ADD CONSTRAINT otp_registration_data CHECK (
        (purpose = 'register' AND reg_password_hash IS NOT NULL AND reg_name IS NOT NULL AND reg_email IS NOT NULL)
        OR (purpose = 'reset' AND reg_password_hash IS NULL AND reg_name IS NULL AND reg_email IS NULL)
    );

-- +goose Down
ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_registration_data,
    DROP COLUMN reg_password_hash,
    DROP COLUMN reg_email,
    DROP COLUMN reg_name,
    DROP COLUMN purpose;
ALTER TABLE users DROP COLUMN password_hash;
