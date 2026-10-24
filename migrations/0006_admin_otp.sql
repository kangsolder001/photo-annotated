-- +goose Up
-- Login superadmin lewat OTP WhatsApp (spec-labels-admin §3.4). Challenge admin memakai antrean,
-- dispatcher, dan kode turunan yang sama dengan OTP pengguna, tetapi tidak terikat ke baris users.

ALTER TABLE otp_challenges ALTER COLUMN user_id DROP NOT NULL;

ALTER TABLE otp_challenges DROP CONSTRAINT otp_challenges_purpose_check;
ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_purpose_check CHECK (purpose IN ('register', 'reset', 'admin_login')),
    -- challenge admin tidak punya pengguna; challenge pengguna selalu punya
    ADD CONSTRAINT otp_admin_has_no_user CHECK ((purpose = 'admin_login') = (user_id IS NULL));

ALTER TABLE otp_challenges DROP CONSTRAINT otp_registration_data;
ALTER TABLE otp_challenges ADD CONSTRAINT otp_registration_data CHECK (
    (purpose = 'register' AND reg_password_hash IS NOT NULL AND reg_name IS NOT NULL AND reg_email IS NOT NULL)
    OR (purpose IN ('reset', 'admin_login') AND reg_password_hash IS NULL AND reg_name IS NULL AND reg_email IS NULL)
);

-- Satu challenge aktif per nomor, dipisah per jenis: login admin dan login/reset pengguna pada nomor
-- yang sama tidak saling membatalkan.
DROP INDEX otp_one_active_per_phone;
CREATE UNIQUE INDEX otp_one_active_per_phone ON otp_challenges (phone, (purpose = 'admin_login'))
    WHERE status IN ('queued', 'sending', 'sent');

-- +goose Down
DELETE FROM otp_challenges WHERE purpose = 'admin_login';
DROP INDEX otp_one_active_per_phone;
CREATE UNIQUE INDEX otp_one_active_per_phone ON otp_challenges (phone) WHERE status IN ('queued', 'sending', 'sent');
ALTER TABLE otp_challenges DROP CONSTRAINT otp_registration_data;
ALTER TABLE otp_challenges ADD CONSTRAINT otp_registration_data CHECK (
    (purpose = 'register' AND reg_password_hash IS NOT NULL AND reg_name IS NOT NULL AND reg_email IS NOT NULL)
    OR (purpose = 'reset' AND reg_password_hash IS NULL AND reg_name IS NULL AND reg_email IS NULL)
);
ALTER TABLE otp_challenges DROP CONSTRAINT otp_admin_has_no_user;
ALTER TABLE otp_challenges DROP CONSTRAINT otp_challenges_purpose_check;
ALTER TABLE otp_challenges ADD CONSTRAINT otp_challenges_purpose_check CHECK (purpose IN ('register', 'reset'));
ALTER TABLE otp_challenges ALTER COLUMN user_id SET NOT NULL;
