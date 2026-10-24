-- +goose Up
-- Hapus permanen oleh admin (spec-labels-admin §5.5, §6.4). deleted_at diisi di transaksi yang sama
-- dengan penandaan foto; sejak itu submission tidak terlihat di mana pun. Baris ini hanya hidup sampai
-- file fotonya selesai dihapus (biasanya di request yang sama, sisanya oleh cleanup).
ALTER TABLE submissions ADD COLUMN deleted_at timestamptz NULL;
CREATE INDEX submissions_deleted_idx ON submissions (deleted_at) WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX submissions_deleted_idx;
ALTER TABLE submissions DROP COLUMN deleted_at;
