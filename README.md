# Order Hub

通用订单中台 Phase 1：管订单生命周期与金额快照，编排 Auth / OfferHub / Ledger Hub。

分层：`iface`（HTTP）→ `application`（Saga）→ `domain`（状态机 / 金额）→ `infra`（DB / Redis / RPC）。

## 百万创建量设计

- API 无状态多副本；`cmd/worker` 独立做超时关单与 Outbox 投递
- `order_id` 使用本地 Snowflake，避免数据库序列热点
- 先远程占用（优惠 reserve / 账本 Freeze / 库存 Reserve），再本地事务落单
- 落单单事务写入 order + lines（批量）+ refs + outbox + 幂等记录
- 状态迁移必须 `WHERE status=? AND version=?` 条件更新
- Preview 只走 Redis/内存，不落库
- 列表使用 keyset cursor，禁止深分页 OFFSET
- 超时关单走部分索引 `PENDING_PAY + expire_at`；多 worker 靠 CAS 抢胜
- Postgres 连接池 / GORM PrepareStmt；Redis 连接池
- 下游 RPC 800ms 超时；失败逆序补偿，补偿幂等

## 本地运行

```bash
docker compose up -d          # 可选：Postgres + Redis
make test
MOCK_DEPENDENCIES=true POSTGRES_DSN=memory make run-api
```

Mock 鉴权：`Authorization: Bearer mock.u_123.tenant_001`

### Preview

```bash
curl -s localhost:8080/api/v1/orders/preview \
  -H 'Authorization: Bearer mock.u_123.tenant_001' \
  -H 'X-Tenant-Id: tenant_001' \
  -H 'Content-Type: application/json' \
  -d '{"scene":"mall_checkout","channel":"app","coupon_ids":["coupon_001"],"items":[{"line_id":"line_1","object_type":"sku","object_id":"sku_1001","quantity":2,"unit_price":10000},{"line_id":"line_2","object_type":"service","object_id":"delivery","quantity":1,"unit_price":6800}]}'
```

### 下单

```bash
curl -s localhost:8080/api/v1/orders \
  -H 'Authorization: Bearer mock.u_123.tenant_001' \
  -H 'Idempotency-Key: cli_001' \
  -H 'Content-Type: application/json' \
  -d '{"client_order_id":"cli_001","scene":"mall_checkout","channel":"app","coupon_ids":["coupon_001"],"items":[{"line_id":"line_1","object_type":"sku","object_id":"sku_1001","quantity":2,"unit_price":10000},{"line_id":"line_2","object_type":"service","object_id":"delivery","quantity":1,"unit_price":6800}]}'
```

支付回告（内网）：

```http
POST /internal/v1/orders/callbacks/payment
{"order_id":"ord_...","tenant_id":"tenant_001","success":true}
```

纯积分场景 `point_mall` 下单后调用 `POST /api/v1/orders/{id}/confirm-ledger`。

## API

前缀 `/api/v1/orders`，响应 `{code,message,request_id,data}`。详见技术方案与 `api/openapi.yaml`。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/orders/preview | 试算，不落单 |
| POST | /api/v1/orders | 下单 |
| GET | /api/v1/orders/{id} | 查询 |
| GET | /api/v1/orders | 列表（cursor） |
| POST | /api/v1/orders/{id}/cancel | 取消 |
| POST | /api/v1/orders/{id}/pay-intent | 重新拉起支付 |
| POST | /api/v1/orders/{id}/confirm-ledger | 纯账本确认 |
| POST | /api/v1/orders/{id}/complete | 履约完成 |
| POST | /api/v1/orders/{id}/refunds | 售后 |
| POST | /internal/v1/orders/callbacks/payment | 渠道回告 |

## 接入真实中台

设置 `MOCK_DEPENDENCIES=false`，并配置：

- `AUTH_HUB_URL` `POST /api/v1/auth/introspect`
- `OFFER_HUB_URL` `/api/discount/v1/quotes|reservations|...`
- `LEDGER_HUB_URL` Freeze / Capture / Release / Credit
