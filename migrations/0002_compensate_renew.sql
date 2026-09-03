-- compensate / renew columns

ALTER TABLE orders ADD COLUMN IF NOT EXISTS renew_count INT NOT NULL DEFAULT 0;

ALTER TABLE order_refunds ADD COLUMN IF NOT EXISTS ledger_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE order_refunds ADD COLUMN IF NOT EXISTS channel_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE order_refunds ADD COLUMN IF NOT EXISTS lines_json JSONB;

ALTER TABLE saga_compensations ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(64);
ALTER TABLE saga_compensations ADD COLUMN IF NOT EXISTS last_error TEXT;
ALTER TABLE saga_compensations ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_saga_retry ON saga_compensations (status, next_retry_at);
