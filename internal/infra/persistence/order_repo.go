package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrderRepo struct {
	db *gorm.DB
}

func NewOrderRepo(db *gorm.DB) *OrderRepo {
	return &OrderRepo{db: db}
}

func (r *OrderRepo) InsertCheckout(ctx context.Context, rec port.CheckoutPersist) error {
	if rec.Order == nil {
		return domain.ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		po := orderToPO(rec.Order)
		if err := tx.Create(&po).Error; err != nil {
			return mapUnique(err)
		}
		lines := linesToPO(rec.Order)
		if len(lines) > 0 {
			if err := tx.CreateInBatches(lines, 100).Error; err != nil {
				return err
			}
		}
		for _, p := range rec.Order.Promotions {
			alloc, _ := json.Marshal(p.Allocations)
			if err := tx.Create(&OrderPromotionPO{
				OrderID:             rec.Order.OrderID,
				SourceType:          p.SourceType,
				SourceID:            p.SourceID,
				DiscountAmount:      p.DiscountAmount,
				AllocationsJSON:     alloc,
				RuleSnapshotVersion: p.RuleSnapshotVersion,
			}).Error; err != nil {
				return err
			}
		}
		for _, lg := range rec.Order.LedgerLegs {
			if err := tx.Create(&OrderLedgerLegPO{
				OrderID:   rec.Order.OrderID,
				Command:   lg.Command,
				BizNo:     lg.BizNo,
				FreezeID:  lg.FreezeID,
				AssetCode: lg.AssetCode,
				Amount:    lg.Amount,
				Status:    lg.Status,
				CreatedAt: rec.Order.CreatedAt,
			}).Error; err != nil {
				return err
			}
		}
		if rec.Idempotency != nil {
			if err := tx.Create(&IdempotencyPO{
				TenantID:    rec.Idempotency.TenantID,
				Actor:       rec.Idempotency.Actor,
				IdemKey:     rec.Idempotency.Key,
				RequestHash: rec.Idempotency.RequestHash,
				Response:    rec.Idempotency.Response,
				OrderID:     rec.Idempotency.OrderID,
				CreatedAt:   rec.Idempotency.CreatedAt,
			}).Error; err != nil {
				return mapUnique(err)
			}
		}
		payload, _ := json.Marshal(rec.Event)
		return tx.Create(&OutboxPO{
			EventID:   rec.Event.EventID,
			EventType: rec.Event.EventType,
			TenantID:  rec.Event.TenantID,
			Payload:   payload,
			CreatedAt: rec.Event.OccurredAt,
		}).Error
	})
}

func (r *OrderRepo) FindByID(ctx context.Context, tenantID, orderID string) (*domain.Order, error) {
	var po OrderPO
	var err error
	if tenantID == "" {
		err = r.db.WithContext(ctx).Where("order_id = ?", orderID).First(&po).Error
	} else {
		err = r.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", tenantID, orderID).First(&po).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	var lines []OrderLinePO
	if err := r.db.WithContext(ctx).Where("order_id = ?", orderID).Order("id").Find(&lines).Error; err != nil {
		return nil, err
	}
	return poToOrder(po, lines), nil
}

func (r *OrderRepo) FindByClientOrderID(ctx context.Context, tenantID, buyerID, clientOrderID string) (*domain.Order, error) {
	var po OrderPO
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND buyer_user_id = ? AND client_order_id = ?", tenantID, buyerID, clientOrderID).First(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	var lines []OrderLinePO
	if err := r.db.WithContext(ctx).Where("order_id = ?", po.OrderID).Order("id").Find(&lines).Error; err != nil {
		return nil, err
	}
	return poToOrder(po, lines), nil
}

func (r *OrderRepo) FindIdempotency(ctx context.Context, tenantID, actor, key string) (*port.IdempotencyRecord, error) {
	var po IdempotencyPO
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND actor = ? AND idempotency_key = ?", tenantID, actor, key).First(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &port.IdempotencyRecord{
		TenantID:    po.TenantID,
		Actor:       po.Actor,
		Key:         po.IdemKey,
		RequestHash: po.RequestHash,
		Response:    po.Response,
		OrderID:     po.OrderID,
		CreatedAt:   po.CreatedAt,
	}, nil
}

func (r *OrderRepo) UpdateIdempotencyResponse(ctx context.Context, tenantID, actor, key string, resp []byte) error {
	return r.db.WithContext(ctx).Model(&IdempotencyPO{}).
		Where("tenant_id = ? AND actor = ? AND idempotency_key = ?", tenantID, actor, key).
		Update("response", resp).Error
}

func (r *OrderRepo) Transition(ctx context.Context, cmd port.TransitionCmd) (*domain.Order, error) {
	var out *domain.Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"status":     string(cmd.To),
			"version":    gorm.Expr("version + 1"),
			"updated_at": time.Now(),
		}
		if cmd.PaidAmount != nil {
			updates["paid_amount"] = *cmd.PaidAmount
		}
		if cmd.RefundedAdd > 0 {
			updates["refunded_amount"] = gorm.Expr("refunded_amount + ?", cmd.RefundedAdd)
		}
		if cmd.RedemptionID != "" {
			updates["redemption_id"] = cmd.RedemptionID
		}
		if cmd.PaymentIntentID != "" {
			updates["payment_intent_id"] = cmd.PaymentIntentID
		}
		if cmd.PaidAt != nil {
			updates["paid_at"] = cmd.PaidAt
		}
		if cmd.CancelledAt != nil {
			updates["cancelled_at"] = cmd.CancelledAt
		}
		if cmd.CompletedAt != nil {
			updates["completed_at"] = cmd.CompletedAt
		}
		q := tx.Model(&OrderPO{}).Where("tenant_id = ? AND order_id = ? AND version = ?", cmd.TenantID, cmd.OrderID, cmd.Version)
		if len(cmd.From) > 0 {
			st := make([]string, 0, len(cmd.From))
			for _, s := range cmd.From {
				st = append(st, string(s))
			}
			q = q.Where("status IN ?", st)
		}
		res := q.Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrVersionConflict
		}
		if cmd.Event != nil {
			payload, _ := json.Marshal(cmd.Event)
			if err := tx.Create(&OutboxPO{
				EventID:   cmd.Event.EventID,
				EventType: cmd.Event.EventType,
				TenantID:  cmd.Event.TenantID,
				Payload:   payload,
				CreatedAt: cmd.Event.OccurredAt,
			}).Error; err != nil {
				return err
			}
		}
		var po OrderPO
		if err := tx.Where("tenant_id = ? AND order_id = ?", cmd.TenantID, cmd.OrderID).First(&po).Error; err != nil {
			return err
		}
		var lines []OrderLinePO
		if err := tx.Where("order_id = ?", cmd.OrderID).Order("id").Find(&lines).Error; err != nil {
			return err
		}
		out = poToOrder(po, lines)
		return nil
	})
	return out, err
}

func (r *OrderRepo) UpdatePaymentIntent(ctx context.Context, tenantID, orderID, intentID, channel string) error {
	return r.db.WithContext(ctx).Model(&OrderPO{}).
		Where("tenant_id = ? AND order_id = ?", tenantID, orderID).
		Updates(map[string]any{"payment_intent_id": intentID, "payment_channel": channel, "updated_at": time.Now()}).Error
}

func (r *OrderRepo) UpdateRedemption(ctx context.Context, tenantID, orderID, redemptionID string) error {
	return r.db.WithContext(ctx).Model(&OrderPO{}).
		Where("tenant_id = ? AND order_id = ?", tenantID, orderID).
		Updates(map[string]any{"redemption_id": redemptionID, "updated_at": time.Now()}).Error
}

func (r *OrderRepo) ListExpiredPending(ctx context.Context, now time.Time, limit int) ([]domain.Order, error) {
	if limit <= 0 {
		limit = 100
	}
	var pos []OrderPO
	err := r.db.WithContext(ctx).
		Where("status = ? AND expire_at < ?", string(domain.StatusPendingPay), now).
		Order("expire_at").
		Limit(limit).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Find(&pos).Error
	if err != nil {
		// SKIP LOCKED 在非 PG 或不在事务中可能失败，降级为无锁扫描。
		err = r.db.WithContext(ctx).
			Where("status = ? AND expire_at < ?", string(domain.StatusPendingPay), now).
			Order("expire_at").Limit(limit).Find(&pos).Error
		if err != nil {
			return nil, err
		}
	}
	out := make([]domain.Order, 0, len(pos))
	for _, po := range pos {
		out = append(out, *poToOrder(po, nil))
	}
	return out, nil
}

func (r *OrderRepo) ListByBuyer(ctx context.Context, tenantID, buyerID string, status domain.Status, scene, cursor string, limit int) ([]domain.Order, string, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND buyer_user_id = ?", tenantID, buyerID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	if scene != "" {
		q = q.Where("scene = ?", scene)
	}
	if cursor != "" {
		created, id, ok := splitCursor(cursor)
		if ok {
			q = q.Where("(created_at, order_id) < (?, ?)", created, id)
		}
	}
	var pos []OrderPO
	if err := q.Order("created_at DESC, order_id DESC").Limit(limit + 1).Find(&pos).Error; err != nil {
		return nil, "", err
	}
	next := ""
	if len(pos) > limit {
		last := pos[limit-1]
		next = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.OrderID
		pos = pos[:limit]
	}
	out := make([]domain.Order, 0, len(pos))
	for _, po := range pos {
		out = append(out, *poToOrder(po, nil))
	}
	return out, next, nil
}

func (r *OrderRepo) AdminList(ctx context.Context, f port.AdminListFilter) ([]domain.Order, string, error) {
	q := r.db.WithContext(ctx).Model(&OrderPO{})
	if f.TenantID != "" {
		q = q.Where("tenant_id = ?", f.TenantID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", string(f.Status))
	}
	if f.Scene != "" {
		q = q.Where("scene = ?", f.Scene)
	}
	if f.Query != "" {
		like := f.Query
		q = q.Where("order_id = ? OR client_order_id = ? OR buyer_user_id = ?", like, like, like)
	}
	if f.Cursor != "" {
		created, id, ok := splitCursor(f.Cursor)
		if ok {
			q = q.Where("(created_at, order_id) < (?, ?)", created, id)
		}
	}
	var pos []OrderPO
	if err := q.Order("created_at DESC, order_id DESC").Limit(f.Limit + 1).Find(&pos).Error; err != nil {
		return nil, "", err
	}
	next := ""
	if len(pos) > f.Limit {
		last := pos[f.Limit-1]
		next = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.OrderID
		pos = pos[:f.Limit]
	}
	out := make([]domain.Order, 0, len(pos))
	for _, po := range pos {
		out = append(out, *poToOrder(po, nil))
	}
	return out, next, nil
}

func (r *OrderRepo) CountOrdersByStatus(ctx context.Context, tenantID string) (map[string]int64, error) {
	q := r.db.WithContext(ctx).Model(&OrderPO{})
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var rows []struct {
		Status string
		N      int64
	}
	if err := q.Select("status, count(*) as n").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

func (r *OrderRepo) CountCompensationsByStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		N      int64
	}
	if err := r.db.WithContext(ctx).Model(&CompensationPO{}).Select("status, count(*) as n").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

func (r *OrderRepo) CountUnpublishedEvents(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&OutboxPO{}).Where("published_at IS NULL").Count(&n).Error
	return n, err
}

func (r *OrderRepo) InsertRefund(ctx context.Context, o *domain.Order, refund domain.Refund, event domain.Event) error {
	lines, _ := json.Marshal(refund.Lines)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&RefundPO{
		RefundID:      refund.RefundID,
		OrderID:       refund.OrderID,
		TenantID:      refund.TenantID,
		Amount:        refund.Amount,
		Currency:      refund.Currency,
		Status:        refund.Status,
		Reason:        refund.Reason,
		ChannelRefund: refund.ChannelRefund,
		LedgerCredit:  refund.LedgerCredit,
		LedgerAmount:  refund.LedgerAmount,
		ChannelAmount: refund.ChannelAmount,
		LinesJSON:     lines,
		CreatedAt:     refund.CreatedAt,
	}).Error
}

func (r *OrderRepo) InsertCompensation(ctx context.Context, t port.CompensationTicket) error {
	if t.Status == "" {
		t.Status = "pending"
	}
	return r.db.WithContext(ctx).Create(&CompensationPO{
		Kind:      t.Kind,
		TenantID:  t.TenantID,
		Ref:       t.Ref,
		Payload:   t.Payload,
		Status:    t.Status,
		Attempts:  0,
		CreatedAt: time.Now(),
	}).Error
}

func (r *OrderRepo) ClaimCompensations(ctx context.Context, now time.Time, limit int) ([]port.CompensationTicket, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []CompensationPO
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", "pending", now).
			Order("id").Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]int64, 0, len(rows))
		for i := range rows {
			ids = append(ids, rows[i].ID)
			rows[i].Attempts++
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Model(&CompensationPO{}).Where("id IN ?", ids).
			Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "status": "running"}).Error
	})
	if err != nil {
		// 无事务锁时降级扫描
		if e := r.db.WithContext(ctx).Where("status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", []string{"pending", "running"}, now).
			Order("id").Limit(limit).Find(&rows).Error; e != nil {
			return nil, e
		}
	}
	out := make([]port.CompensationTicket, 0, len(rows))
	for _, row := range rows {
		out = append(out, poToComp(row))
	}
	return out, nil
}

func (r *OrderRepo) UpdateCompensation(ctx context.Context, id int64, status, lastErr string, nextRetry *time.Time) error {
	updates := map[string]any{"status": status, "last_error": lastErr}
	if nextRetry != nil {
		updates["next_retry_at"] = nextRetry
	}
	if status == "done" || status == "failed" {
		now := time.Now()
		updates["done_at"] = &now
	}
	return r.db.WithContext(ctx).Model(&CompensationPO{}).Where("id = ?", id).Updates(updates).Error
}

func (r *OrderRepo) ListCompensations(ctx context.Context, status string, limit int) ([]port.CompensationTicket, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Order("id DESC").Limit(limit)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []CompensationPO
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]port.CompensationTicket, 0, len(rows))
	for _, row := range rows {
		out = append(out, poToComp(row))
	}
	return out, nil
}

func (r *OrderRepo) FindCompensation(ctx context.Context, id int64) (*port.CompensationTicket, error) {
	var row CompensationPO
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, err
	}
	t := poToComp(row)
	return &t, nil
}

func (r *OrderRepo) ListPendingPayForRenew(ctx context.Context, now time.Time, window time.Duration, maxRenew, limit int) ([]domain.Order, error) {
	if limit <= 0 {
		limit = 100
	}
	deadline := now.Add(window)
	var pos []OrderPO
	err := r.db.WithContext(ctx).
		Where("status = ? AND reservation_id <> '' AND expire_at > ? AND expire_at <= ? AND renew_count < ?",
			string(domain.StatusPendingPay), now, deadline, maxRenew).
		Order("expire_at").Limit(limit).Find(&pos).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.Order, 0, len(pos))
	for _, po := range pos {
		out = append(out, *poToOrder(po, nil))
	}
	return out, nil
}

func (r *OrderRepo) BumpRenew(ctx context.Context, tenantID, orderID string, expectedCount int) error {
	res := r.db.WithContext(ctx).Model(&OrderPO{}).
		Where("tenant_id = ? AND order_id = ? AND status = ? AND renew_count = ?", tenantID, orderID, string(domain.StatusPendingPay), expectedCount).
		Updates(map[string]any{"renew_count": gorm.Expr("renew_count + 1"), "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrVersionConflict
	}
	return nil
}

func poToComp(row CompensationPO) port.CompensationTicket {
	return port.CompensationTicket{
		ID:        row.ID,
		Kind:      row.Kind,
		TenantID:  row.TenantID,
		Ref:       row.Ref,
		Payload:   row.Payload,
		Status:    row.Status,
		Attempts:  row.Attempts,
		LastError: row.LastError,
		NextRetry: row.NextRetry,
		CreatedAt: row.CreatedAt,
	}
}

func (r *OrderRepo) ListUnpublishedEvents(ctx context.Context, limit int) ([]port.OutboxRow, error) {
	var rows []OutboxPO
	err := r.db.WithContext(ctx).Where("published_at IS NULL").Order("created_at").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]port.OutboxRow, 0, len(rows))
	for _, r0 := range rows {
		out = append(out, port.OutboxRow{EventID: r0.EventID, Payload: r0.Payload, Attempts: r0.Attempts})
	}
	return out, nil
}

func (r *OrderRepo) MarkEventPublished(ctx context.Context, eventID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&OutboxPO{}).Where("event_id = ?", eventID).
		Updates(map[string]any{"published_at": now, "attempts": gorm.Expr("attempts + 1")}).Error
}

func (r *OrderRepo) ListForReconcile(ctx context.Context, tenantID string, limit int) ([]domain.Order, error) {
	if limit <= 0 {
		limit = 200
	}
	q := r.db.WithContext(ctx).Where("reservation_id <> '' OR ledger_pay_amount > 0")
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var pos []OrderPO
	if err := q.Order("updated_at DESC").Limit(limit).Find(&pos).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Order, 0, len(pos))
	for _, po := range pos {
		out = append(out, *poToOrder(po, nil))
	}
	return out, nil
}

func (r *OrderRepo) HasOpenCompensation(ctx context.Context, kind, tenantID, ref string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&CompensationPO{}).
		Where("kind = ? AND tenant_id = ? AND ref = ? AND status IN ?", kind, tenantID, ref, []string{"pending", "running"}).
		Count(&n).Error
	return n > 0, err
}

func mapUnique(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "uk_orders_client") || strings.Contains(msg, "client_order_id") {
		return domain.ErrClientOrderConflict
	}
	if strings.Contains(msg, "uk_idem") || strings.Contains(msg, "idempotency") {
		return domain.ErrIdempotencyConflict
	}
	if strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") {
		return domain.ErrClientOrderConflict
	}
	return err
}

func splitCursor(cursor string) (time.Time, string, bool) {
	i := strings.LastIndex(cursor, "|")
	if i <= 0 {
		return time.Time{}, "", false
	}
	t, err := time.Parse(time.RFC3339Nano, cursor[:i])
	if err != nil {
		return time.Time{}, "", false
	}
	return t, cursor[i+1:], true
}

func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("postgres dsn required")
	}
	db, err := gorm.Open(postgresDialector(dsn), &gorm.Config{
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(64)
	sqlDB.SetMaxIdleConns(16)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}
