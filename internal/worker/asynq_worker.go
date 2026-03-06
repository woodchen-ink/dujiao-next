package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/provider"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/service"

	"github.com/hibiken/asynq"
)

// Consumer 异步任务消费者
type Consumer struct {
	*provider.Container
}

// NewConsumer 创建消费者
func NewConsumer(c *provider.Container) *Consumer {
	return &Consumer{
		Container: c,
	}
}

// Register 注册消费者
func (c *Consumer) Register(mux *asynq.ServeMux) {
	if c == nil || mux == nil {
		logger.Debugw("worker_register_skip_nil", "consumer_nil", c == nil, "mux_nil", mux == nil)
		return
	}
	mux.HandleFunc(queue.TaskOrderStatusEmail, c.handleOrderStatusEmail)
	mux.HandleFunc(queue.TaskOrderAutoFulfill, c.handleOrderAutoFulfill)
	mux.HandleFunc(queue.TaskOrderTimeoutCancel, c.handleOrderTimeoutCancel)
	mux.HandleFunc(queue.TaskWalletRechargeExpire, c.handleWalletRechargeExpire)
	mux.HandleFunc(queue.TaskNotificationDispatch, c.handleNotificationDispatch)
	mux.HandleFunc(queue.TaskAffiliateConfirmCommissions, c.handleAffiliateConfirmCommissions)
	mux.HandleFunc(queue.TaskUpstreamSyncStock, c.handleUpstreamSyncStock)
	mux.HandleFunc(queue.TaskProcurementSubmit, c.handleProcurementSubmit)
	mux.HandleFunc(queue.TaskProcurementPollStatus, c.handleProcurementPollStatus)
	mux.HandleFunc(queue.TaskDownstreamCallback, c.handleDownstreamCallback)
	mux.HandleFunc(queue.TaskReconciliationRun, c.handleReconciliationRun)
}

func (c *Consumer) handleOrderStatusEmail(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		logger.Debugw("worker_order_status_email_skip_nil", "consumer_nil", c == nil, "task_nil", task == nil)
		return nil
	}
	var payload queue.OrderStatusEmailPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_order_status_email_unmarshal_failed", "error", err)
		return err
	}
	if payload.OrderID == 0 {
		logger.Debugw("worker_order_status_email_skip_invalid_payload", "order_id", payload.OrderID)
		return nil
	}
	order, err := c.OrderRepo.GetByID(payload.OrderID)
	if err != nil {
		logger.Warnw("worker_order_status_email_fetch_order_failed", "order_id", payload.OrderID, "error", err)
		return err
	}
	if order == nil {
		logger.Debugw("worker_order_status_email_skip_order_not_found", "order_id", payload.OrderID)
		return nil
	}
	var receiverEmail string
	var locale string
	if order.UserID != 0 {
		user, err := c.UserRepo.GetByID(order.UserID)
		if err != nil {
			logger.Warnw("worker_order_status_email_fetch_user_failed", "order_id", order.ID, "user_id", order.UserID, "error", err)
			return err
		}
		if user != nil {
			receiverEmail = strings.TrimSpace(user.Email)
			locale = strings.TrimSpace(user.Locale)
		}
	} else {
		receiverEmail = strings.TrimSpace(order.GuestEmail)
		locale = strings.TrimSpace(order.GuestLocale)
	}
	if receiverEmail == "" {
		logger.Debugw("worker_order_status_email_skip_empty_receiver", "order_id", order.ID, "order_no", order.OrderNo)
		return nil
	}
	if isTelegramPlaceholderReceiver(receiverEmail) {
		logger.Debugw("worker_order_status_email_skip_placeholder_receiver", "order_id", order.ID, "order_no", order.OrderNo)
		return nil
	}
	if c.EmailService == nil {
		logger.Warnw("worker_order_status_email_skip_email_service_nil", "order_id", order.ID, "order_no", order.OrderNo)
		return nil
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = order.Status
	}
	payloadText := buildOrderFulfillmentEmailPayload(order)
	input := service.OrderStatusEmailInput{
		OrderNo:         order.OrderNo,
		Status:          status,
		Amount:          order.TotalAmount,
		Currency:        order.Currency,
		FulfillmentInfo: payloadText,
		IsGuest:         order.UserID == 0,
	}
	if err := c.EmailService.SendOrderStatusEmail(receiverEmail, input, locale); err != nil {
		logger.Warnw("worker_order_status_email_send_failed",
			"order_id", order.ID,
			"order_no", order.OrderNo,
			"receiver_email", receiverEmail,
			"status", status,
			"error", err,
		)
		return err
	}
	return nil
}

func (c *Consumer) handleOrderAutoFulfill(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		logger.Debugw("worker_order_auto_fulfill_skip_nil", "consumer_nil", c == nil, "task_nil", task == nil)
		return nil
	}
	var payload queue.OrderAutoFulfillPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_order_auto_fulfill_unmarshal_failed", "error", err)
		return err
	}
	if payload.OrderID == 0 {
		logger.Debugw("worker_order_auto_fulfill_skip_invalid_payload", "order_id", payload.OrderID)
		return nil
	}
	_, err := c.FulfillmentService.CreateAuto(payload.OrderID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFulfillmentExists):
			logger.Debugw("worker_order_auto_fulfill_skip_exists", "order_id", payload.OrderID)
			return nil
		case errors.Is(err, service.ErrFulfillmentNotAuto):
			logger.Debugw("worker_order_auto_fulfill_skip_not_auto", "order_id", payload.OrderID)
			return nil
		case errors.Is(err, service.ErrOrderStatusInvalid):
			logger.Debugw("worker_order_auto_fulfill_skip_invalid_status", "order_id", payload.OrderID)
			return nil
		case errors.Is(err, service.ErrOrderNotFound):
			logger.Debugw("worker_order_auto_fulfill_skip_order_not_found", "order_id", payload.OrderID)
			return nil
		default:
			logger.Warnw("worker_order_auto_fulfill_failed", "order_id", payload.OrderID, "error", err)
			return err
		}
	}
	return nil
}

func (c *Consumer) handleOrderTimeoutCancel(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		logger.Debugw("worker_order_timeout_cancel_skip_nil", "consumer_nil", c == nil, "task_nil", task == nil)
		return nil
	}
	var payload queue.OrderTimeoutCancelPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_order_timeout_cancel_unmarshal_failed", "error", err)
		return err
	}
	if payload.OrderID == 0 {
		logger.Debugw("worker_order_timeout_cancel_skip_invalid_payload", "order_id", payload.OrderID)
		return nil
	}
	if c.OrderService == nil {
		logger.Warnw("worker_order_timeout_cancel_skip_order_service_nil", "order_id", payload.OrderID)
		return nil
	}
	_, err := c.OrderService.CancelExpiredOrder(payload.OrderID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOrderNotFound):
			logger.Debugw("worker_order_timeout_cancel_skip_order_not_found", "order_id", payload.OrderID)
			return nil
		case errors.Is(err, service.ErrOrderFetchFailed):
			logger.Warnw("worker_order_timeout_cancel_fetch_failed", "order_id", payload.OrderID, "error", err)
			return nil
		case errors.Is(err, service.ErrOrderUpdateFailed):
			logger.Warnw("worker_order_timeout_cancel_update_failed", "order_id", payload.OrderID, "error", err)
			return err
		default:
			logger.Warnw("worker_order_timeout_cancel_failed", "order_id", payload.OrderID, "error", err)
			return err
		}
	}
	return nil
}

func (c *Consumer) handleWalletRechargeExpire(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		logger.Debugw("worker_wallet_recharge_expire_skip_nil", "consumer_nil", c == nil, "task_nil", task == nil)
		return nil
	}
	var payload queue.WalletRechargeExpirePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_wallet_recharge_expire_unmarshal_failed", "error", err)
		return err
	}
	if payload.PaymentID == 0 {
		logger.Debugw("worker_wallet_recharge_expire_skip_invalid_payload", "payment_id", payload.PaymentID)
		return nil
	}
	if c.PaymentService == nil {
		logger.Warnw("worker_wallet_recharge_expire_skip_payment_service_nil", "payment_id", payload.PaymentID)
		return nil
	}
	if _, err := c.PaymentService.ExpireWalletRechargePayment(payload.PaymentID); err != nil {
		switch {
		case errors.Is(err, service.ErrPaymentNotFound):
			logger.Debugw("worker_wallet_recharge_expire_skip_payment_not_found", "payment_id", payload.PaymentID)
			return nil
		case errors.Is(err, service.ErrWalletRechargeNotFound):
			logger.Debugw("worker_wallet_recharge_expire_skip_recharge_not_found", "payment_id", payload.PaymentID)
			return nil
		case errors.Is(err, service.ErrPaymentUpdateFailed):
			logger.Warnw("worker_wallet_recharge_expire_update_failed", "payment_id", payload.PaymentID, "error", err)
			return err
		default:
			logger.Warnw("worker_wallet_recharge_expire_failed", "payment_id", payload.PaymentID, "error", err)
			return err
		}
	}
	return nil
}

func (c *Consumer) handleNotificationDispatch(ctx context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		logger.Debugw("worker_notification_dispatch_skip_nil", "consumer_nil", c == nil, "task_nil", task == nil)
		return nil
	}
	if c.NotificationService == nil {
		logger.Warnw("worker_notification_dispatch_skip_service_nil")
		return nil
	}
	var payload queue.NotificationDispatchPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_notification_dispatch_unmarshal_failed", "error", err)
		return err
	}
	if strings.TrimSpace(payload.EventType) == "" {
		logger.Debugw("worker_notification_dispatch_skip_empty_event")
		return nil
	}
	if err := c.NotificationService.Dispatch(ctx, payload); err != nil {
		logger.Warnw("worker_notification_dispatch_failed",
			"event_type", payload.EventType,
			"biz_type", payload.BizType,
			"biz_id", payload.BizID,
			"error", err,
		)
		return err
	}
	return nil
}

func (c *Consumer) handleAffiliateConfirmCommissions(_ context.Context, _ *asynq.Task) error {
	if c == nil || c.AffiliateService == nil {
		logger.Debugw("worker_affiliate_confirm_skip_nil", "consumer_nil", c == nil)
		return nil
	}
	if err := c.AffiliateService.ConfirmDueCommissions(time.Now()); err != nil {
		logger.Warnw("worker_affiliate_confirm_due_failed", "error", err)
		return err
	}
	return nil
}

func (c *Consumer) handleUpstreamSyncStock(_ context.Context, _ *asynq.Task) error {
	if c == nil || c.ProductMappingService == nil {
		logger.Debugw("worker_upstream_sync_stock_skip_nil", "consumer_nil", c == nil)
		return nil
	}
	if err := c.ProductMappingService.SyncAllStock(); err != nil {
		logger.Warnw("worker_upstream_sync_stock_failed", "error", err)
		return err
	}
	return nil
}

func (c *Consumer) handleProcurementSubmit(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil || c.ProcurementOrderService == nil {
		logger.Debugw("worker_procurement_submit_skip_nil")
		return nil
	}
	var payload queue.ProcurementSubmitPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_procurement_submit_unmarshal_failed", "error", err)
		return err
	}
	if payload.ProcurementOrderID == 0 {
		return nil
	}
	if err := c.ProcurementOrderService.SubmitToUpstream(payload.ProcurementOrderID); err != nil {
		logger.Warnw("worker_procurement_submit_failed",
			"procurement_order_id", payload.ProcurementOrderID,
			"error", err,
		)
		return err
	}
	return nil
}

func (c *Consumer) handleProcurementPollStatus(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil || c.ProcurementOrderService == nil {
		logger.Debugw("worker_procurement_poll_skip_nil")
		return nil
	}
	var payload queue.ProcurementPollStatusPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_procurement_poll_unmarshal_failed", "error", err)
		return err
	}
	if payload.ProcurementOrderID == 0 {
		return nil
	}
	if err := c.ProcurementOrderService.PollUpstreamStatus(payload.ProcurementOrderID); err != nil {
		logger.Warnw("worker_procurement_poll_failed",
			"procurement_order_id", payload.ProcurementOrderID,
			"error", err,
		)
		return err
	}
	return nil
}

func (c *Consumer) handleDownstreamCallback(_ context.Context, task *asynq.Task) error {
	if c == nil || task == nil || c.DownstreamCallbackService == nil {
		logger.Debugw("worker_downstream_callback_skip_nil")
		return nil
	}
	var payload queue.DownstreamCallbackPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_downstream_callback_unmarshal_failed", "error", err)
		return err
	}
	if payload.DownstreamOrderRefID == 0 {
		return nil
	}
	if err := c.DownstreamCallbackService.SendCallback(payload.DownstreamOrderRefID); err != nil {
		logger.Warnw("worker_downstream_callback_failed",
			"ref_id", payload.DownstreamOrderRefID,
			"error", err,
		)
		return err
	}
	return nil
}

func (c *Consumer) handleReconciliationRun(ctx context.Context, task *asynq.Task) error {
	if c == nil || task == nil || c.ReconciliationService == nil {
		logger.Debugw("worker_reconciliation_run_skip_nil")
		return nil
	}
	var payload queue.ReconciliationRunPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_reconciliation_run_unmarshal_failed", "error", err)
		return err
	}
	if payload.JobID == 0 {
		return nil
	}
	if err := c.ReconciliationService.Execute(ctx, payload.JobID); err != nil {
		logger.Warnw("worker_reconciliation_run_failed",
			"job_id", payload.JobID,
			"error", err,
		)
		return err
	}
	return nil
}

func isTelegramPlaceholderReceiver(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return false
	}
	return strings.HasPrefix(normalized, "telegram_") && strings.HasSuffix(normalized, "@login.local")
}

func buildOrderFulfillmentEmailPayload(order *models.Order) string {
	if order == nil {
		return ""
	}
	if order.Fulfillment != nil {
		payload := strings.TrimSpace(order.Fulfillment.Payload)
		if payload != "" {
			return payload
		}
	}
	if len(order.Children) == 0 {
		return ""
	}
	parts := make([]string, 0, len(order.Children))
	for _, child := range order.Children {
		if child.Fulfillment == nil {
			continue
		}
		content := strings.TrimSpace(child.Fulfillment.Payload)
		if content == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]\n%s", strings.TrimSpace(child.OrderNo), content))
	}
	return strings.Join(parts, "\n\n")
}
