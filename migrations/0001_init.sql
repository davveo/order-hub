-- Order Hub Phase 1 schema
-- 写入路径按 snowflake order_id 无序列热点；超时关单走部分索引；列表走 keyset。
-- 百万级以上可将 orders 按 created_at 月分区；唯一键 uk_orders_client 保留在独立小表以免分区唯一约束必须带分区键。

CREATE TABLE IF NOT EXISTS orders (
    order_id           VARCHAR(64) PRIMARY KEY,
    tenant_id          VARCHAR(64)  NOT NULL,
    scene              VARCHAR(32)  NOT NULL,
    channel            VARCHAR(32)  NOT NULL,
    buyer_user_id      VARCHAR(64)  NOT NULL,
    client_order_id    VARCHAR(128) NOT NULL,
    status             VARCHAR(32)  NOT NULL,
    version            BIGINT       NOT NULL DEFAULT 1,
    currency           VARCHAR(16)  NOT NULL,
    original_amount    BIGINT       NOT NULL,
    discount_amount    BIGINT       NOT NULL,
    payable_amount     BIGINT       NOT NULL,
    ledger_pay_amount  BIGINT       NOT NULL DEFAULT 0,
    channel_pay_amount BIGINT       NOT NULL DEFAULT 0,
    paid_amount        BIGINT       NOT NULL DEFAULT 0,
    refunded_amount    BIGINT       NOT NULL DEFAULT 0,
    quote_id           VARCHAR(64),
    reservation_id     VARCHAR(64),
    redemption_id      VARCHAR(64),
    freeze_id          VARCHAR(64),
    asset_code         VARCHAR(32),
    pay_method         VARCHAR(16)  NOT NULL,
    payment_intent_id  VARCHAR(64),
    payment_channel    VARCHAR(32),
    expire_at          TIMESTAMPTZ,
    context_json       JSONB,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    paid_at            TIMESTAMPTZ,
    cancelled_at       TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_orders_client
    ON orders (tenant_id, buyer_user_id, client_order_id);
CREATE INDEX IF NOT EXISTS idx_orders_buyer
    ON orders (tenant_id, buyer_user_id, created_at DESC, order_id DESC);
CREATE INDEX IF NOT EXISTS idx_orders_timeout
    ON orders (expire_at)
    WHERE status = 'PENDING_PAY';
CREATE INDEX IF NOT EXISTS idx_orders_created_brin
    ON orders USING BRIN (created_at);

CREATE TABLE IF NOT EXISTS order_lines (
    id               BIGSERIAL PRIMARY KEY,
    order_id         VARCHAR(64) NOT NULL REFERENCES orders(order_id),
    line_id          VARCHAR(64) NOT NULL,
    object_type      VARCHAR(32) NOT NULL,
    object_id        VARCHAR(64) NOT NULL,
    quantity         BIGINT      NOT NULL,
    unit_price       BIGINT      NOT NULL,
    original_amount  BIGINT      NOT NULL,
    discount_amount  BIGINT      NOT NULL DEFAULT 0,
    payable_amount   BIGINT      NOT NULL,
    attributes_json  JSONB,
    snapshot_json    JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (order_id, line_id)
);

CREATE TABLE IF NOT EXISTS order_promotions (
    id                    BIGSERIAL PRIMARY KEY,
    order_id              VARCHAR(64) NOT NULL,
    source_type           VARCHAR(32),
    source_id             VARCHAR(64),
    discount_amount       BIGINT      NOT NULL DEFAULT 0,
    allocations_json      JSONB,
    rule_snapshot_version VARCHAR(64)
);
CREATE INDEX IF NOT EXISTS idx_order_promotions_order ON order_promotions (order_id);

CREATE TABLE IF NOT EXISTS order_ledger_legs (
    id         BIGSERIAL PRIMARY KEY,
    order_id   VARCHAR(64) NOT NULL,
    command    VARCHAR(16) NOT NULL,
    biz_no     VARCHAR(128) NOT NULL UNIQUE,
    freeze_id  VARCHAR(64),
    asset_code VARCHAR(32),
    amount     BIGINT      NOT NULL,
    status     VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_events (
    event_id      VARCHAR(64) PRIMARY KEY,
    event_type    VARCHAR(64) NOT NULL,
    tenant_id     VARCHAR(64) NOT NULL,
    payload       JSONB       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ,
    attempts      INT         NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished ON order_events (created_at) WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS idempotency_records (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       VARCHAR(64)  NOT NULL,
    actor           VARCHAR(64)  NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    request_hash    VARCHAR(64)  NOT NULL,
    response        JSONB,
    order_id        VARCHAR(64),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, actor, idempotency_key)
);

CREATE TABLE IF NOT EXISTS order_refunds (
    refund_id       VARCHAR(64) PRIMARY KEY,
    order_id        VARCHAR(64) NOT NULL,
    tenant_id       VARCHAR(64) NOT NULL,
    amount          BIGINT      NOT NULL,
    currency        VARCHAR(16) NOT NULL,
    status          VARCHAR(16) NOT NULL,
    reason          VARCHAR(256),
    channel_refund  BOOLEAN     NOT NULL DEFAULT FALSE,
    ledger_credit   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_refunds_order ON order_refunds (order_id);

CREATE TABLE IF NOT EXISTS saga_compensations (
    id         BIGSERIAL PRIMARY KEY,
    kind       VARCHAR(32) NOT NULL,
    ref        VARCHAR(128),
    payload    TEXT,
    status     VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts   INT         NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_saga_pending ON saga_compensations (status, created_at);
