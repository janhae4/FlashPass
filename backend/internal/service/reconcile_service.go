package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/redis"
	"flashpass/backend/internal/repository"
)

type ReconcileResult struct {
	EventID       string `json:"event_id"`
	TotalTickets  int    `json:"total_tickets"`
	ActiveOrders  int    `json:"active_orders"`
	ExpectedStock int    `json:"expected_stock"`
	ActualStock   int    `json:"actual_stock"`
	Discrepancy   int    `json:"discrepancy"`
	Healed        bool   `json:"healed"`
}

type ReconcileService interface {
	ReconcileEvent(ctx context.Context, eventUUID uuid.UUID) (*ReconcileResult, error)
	TriggerEmergencyAudit(eventID string)
	ReconcileAll(ctx context.Context, queueDepth int, suspected map[string]int) error
}

type reconcileService struct {
	eventRepo  repository.EventRepository
	stockCache redis.StockCache
}

func NewReconcileService(eventRepo repository.EventRepository, stockCache redis.StockCache) ReconcileService {
	return &reconcileService{
		eventRepo:  eventRepo,
		stockCache: stockCache,
	}
}

func (s *reconcileService) ReconcileEvent(ctx context.Context, eventUUID uuid.UUID) (*ReconcileResult, error) {
	eventIDStr := eventUUID.String()
	totalTickets, activeOrders, err := s.eventRepo.GetEventWithActiveCount(ctx, eventUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get event order counts: %w", err)
	}

	expectedStock := max(totalTickets-activeOrders, 0)
	actualStock, err := s.stockCache.GetStock(ctx, eventIDStr)
	if err != nil {
		return &ReconcileResult{
			EventID:       eventIDStr,
			TotalTickets:  totalTickets,
			ActiveOrders:  activeOrders,
			ExpectedStock: expectedStock,
		}, fmt.Errorf("failed to get stock from cache: %w", err)
	}

	discrepancy := expectedStock - actualStock
	healed := false
	if discrepancy != 0 {
		if err := s.stockCache.PrewarmStock(ctx, eventIDStr, expectedStock); err != nil {
			log.Printf("[Reconcile ERROR] Failed to update Redis stock for event %s: %v", eventIDStr, err)
		}
		if err := s.eventRepo.UpdateAvailableTickets(ctx, eventUUID, expectedStock); err != nil {
			log.Printf("[Reconcile ERROR] Failed to update DB available tickets for event %s: %v", eventIDStr, err)
		}
		healed = true
	}

	return &ReconcileResult{
		EventID:       eventIDStr,
		TotalTickets:  totalTickets,
		ActiveOrders:  activeOrders,
		ExpectedStock: expectedStock,
		ActualStock:   actualStock,
		Discrepancy:   discrepancy,
		Healed:        healed,
	}, nil
}

// TriggerEmergencyAudit acquires a 5-second SETNX lock to safely trigger an emergency audit
// right when stock hits zero, avoiding Thundering Herd across concurrent requests
func (s *reconcileService) TriggerEmergencyAudit(eventID string) {
	eventUUID, err := uuid.Parse(eventID)
	if err != nil {
		return
	}

	lockKey := redis.AuditLockKey(eventID)
	acquired, err := s.stockCache.AcquireLock(context.Background(), lockKey, 5*time.Second)
	if err != nil || !acquired {
		return
	}

	go func() {
		// Settle window for in-flight DB writes
		time.Sleep(500 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		res, err := s.ReconcileEvent(ctx, eventUUID)
		if err != nil {
			log.Printf("[Emergency Auditor ERROR] Failed audit for event %s: %v", eventID, err)
			return
		}
		if res.Healed {
			log.Printf("[Emergency Auditor SUCCESS] Instant healed %d ghost tickets right after zero! Event: %s", res.Discrepancy, eventID)
		} else {
			log.Printf("[Emergency Auditor] Verified event %s is genuinely SOLD OUT (discrepancy=%d).", eventID, res.Discrepancy)
		}
	}()
}

// ReconcileAll processes all events for background cron scheduler with queue depth detection & debouncing
func (s *reconcileService) ReconcileAll(ctx context.Context, queueDepth int, suspected map[string]int) error {
	events, err := s.eventRepo.GetAllEventsWithActiveCounts(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch all events: %w", err)
	}

	for _, item := range events {
		eventID := item.EventID.String()
		expectedStock := max(item.TotalTickets-item.ActiveOrders, 0)
		actualStock, err := s.stockCache.GetStock(ctx, eventID)
		if err != nil {
			continue
		}

		discrepancy := expectedStock - actualStock
		if discrepancy == 0 {
			delete(suspected, eventID)
			continue
		}

		if queueDepth == 0 {
			log.Printf("[Reconcile Scheduler INSTANT HEAL] Queue empty (0 in-flight). Discrepancy %d tickets for event %s! Syncing stock to %d...",
				discrepancy, eventID, expectedStock)
			_ = s.stockCache.PrewarmStock(ctx, eventID, expectedStock)
			_ = s.eventRepo.UpdateAvailableTickets(ctx, item.EventID, expectedStock)
			delete(suspected, eventID)
			log.Printf("[Reconcile Scheduler SUCCESS] Event %s healed instantly. Redis stock restored to %d.", eventID, expectedStock)
			continue
		}

		prevDiscrepancy, exists := suspected[eventID]
		if !exists || prevDiscrepancy != discrepancy {
			suspected[eventID] = discrepancy
			log.Printf("[Reconcile Scheduler DEBOUNCE] Queue busy (%d in-flight). Suspected stock discrepancy for event %s: diff=%d. Waiting next cycle...",
				queueDepth, eventID, discrepancy)
		} else {
			log.Printf("[Reconcile Scheduler CONFIRMED HEAL] Confirmed %d stock discrepancy for event %s after 2 cycles! Self-healing...",
				discrepancy, eventID)
			_ = s.stockCache.PrewarmStock(ctx, eventID, expectedStock)
			_ = s.eventRepo.UpdateAvailableTickets(ctx, item.EventID, expectedStock)
			delete(suspected, eventID)
			log.Printf("[Reconcile Scheduler SUCCESS] Event %s healed. Redis stock restored to %d.", eventID, expectedStock)
		}
	}
	return nil
}
