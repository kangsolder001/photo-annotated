-- +goose Up
-- Pengguna, OTP, dan sesi (spec §7.4–7.6), serta FK pemilik pada submissions/photos.

CREATE TABLE users (
    id                uuid        PRIMARY KEY,
    name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    -- email tidak diverifikasi dan tidak unik; disimpan huruf kecil
    email             text        NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254 AND email = lower(email)),
    -- bentuk kanonik nomor seluler Indonesia (sama dengan domain.NormalizePhone)
    phone             text        NOT NULL UNIQUE CHECK (phone ~ '^628[0-9]{8,12}$'),
    status            text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active')),
    phone_verified_at timestamptz NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    last_login_at     timestamptz NULL,
    -- aktif ⇔ nomor sudah diverifikasi
    CONSTRAINT users_active_iff_verified CHECK ((status = 'active') = (phone_verified_at IS NOT NULL))
);

CREATE INDEX users_pending_created_idx ON users (created_at) WHERE status = 'pending';

CREATE TABLE otp_challenges (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    phone           text        NOT NULL CHECK (phone ~ '^628[0-9]{8,12}$'),
    code_hash       bytea       NOT NULL CHECK (octet_length(code_hash) = 32),
    status          text        NOT NULL DEFAULT 'queued'
                                CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'consumed', 'expired')),
    failure_reason  text        NULL CHECK (failure_reason IN ('WA_UNAVAILABLE', 'SEND_FAILED')),
    verify_attempts smallint    NOT NULL DEFAULT 0 CHECK (verify_attempts >= 0),
    send_attempts   smallint    NOT NULL DEFAULT 0 CHECK (send_attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    wa_message_id   text        NULL,
    client_ip       inet        NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz NULL,
    expires_at      timestamptz NULL,
    CONSTRAINT otp_failure_reason_only_when_failed CHECK (failure_reason IS NULL OR status = 'failed'),
    CONSTRAINT otp_expiry_needs_sent CHECK (expires_at IS NULL OR sent_at IS NOT NULL)
);

-- worker mengambil challenge queued berikutnya
CREATE INDEX otp_worker_idx ON otp_challenges (status, next_attempt_at);
-- batas per nomor (cooldown, per jam) dan penghitungan harian
CREATE INDEX otp_phone_created_idx ON otp_challenges (phone, created_at);
CREATE INDEX otp_created_idx ON otp_challenges (created_at);
-- satu nomor hanya boleh punya satu challenge aktif; yang lama ditandai expired sebelum yang baru dibuat
CREATE UNIQUE INDEX otp_one_active_per_phone ON otp_challenges (phone) WHERE status IN ('queued', 'sending', 'sent');

CREATE TABLE sessions (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- SHA-256 dari token acak 32 byte; token asli hanya dikirim sekali ke klien
    token_hash bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz NULL,
    client_ip  inet        NULL,
    CONSTRAINT sessions_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- Pemilik submission/foto (mode otp). Pengguna yang dihapus tidak menghapus datanya.
ALTER TABLE submissions
    ADD CONSTRAINT submissions_owner_fk FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE photos
    ADD CONSTRAINT photos_owner_fk FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE photos DROP CONSTRAINT photos_owner_fk;
ALTER TABLE submissions DROP CONSTRAINT submissions_owner_fk;
DROP TABLE sessions;
DROP TABLE otp_challenges;
DROP TABLE users;
