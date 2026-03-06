package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/repository"
	"github.com/dujiao-next/internal/upstream"

	"github.com/hibiken/asynq"
)

var (
	ErrDownstreamRefNotFound = errors.New("downstream order ref not found")
)

// DownstreamCallbackService B 侧下游回调通知服务
type DownstreamCallbackService struct {
	refRepo        repository.DownstreamOrderRefRepository
	orderRepo      repository.OrderRepository
	credentialRepo repository.ApiCredentialRepository
	queueClient    *queue.Client
	httpClient     *http.Client
}

// NewDownstreamCallbackService 创建下游回调服务
func NewDownstreamCallbackService(
	refRepo repository.DownstreamOrderRefRepository,
	orderRepo repository.OrderRepository,
	credentialRepo repository.ApiCredentialRepository,
	queueClient *queue.Client,
) *DownstreamCallbackService {
	return &DownstreamCallbackService{
		refRepo:        refRepo,
		orderRepo:      orderRepo,
		credentialRepo: credentialRepo,
		queueClient:    queueClient,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// CreateRef 创建下游订单引用
func (s *DownstreamCallbackService) CreateRef(ref *models.DownstreamOrderRef) error {
	if ref == nil || ref.OrderID == 0 {
		return errors.New("invalid downstream order ref")
	}
	ref.CallbackStatus = constants.CallbackStatusPending
	return s.refRepo.Create(ref)
}

// GetByOrderID 根据订单 ID 查询下游引用
func (s *DownstreamCallbackService) GetByOrderID(orderID uint) (*models.DownstreamOrderRef, error) {
	return s.refRepo.GetByOrderID(orderID)
}

// EnqueueCallback 当 B 侧订单状态变更时，检查是否需要回调下游
func (s *DownstreamCallbackService) EnqueueCallback(orderID uint) {
	if s.queueClient == nil {
		return
	}
	ref, err := s.refRepo.GetByOrderID(orderID)
	if err != nil || ref == nil {
		return
	}
	if strings.TrimSpace(ref.CallbackURL) == "" {
		return
	}
	if ref.CallbackStatus == constants.CallbackStatusSent {
		// 已发送过，可能需要重新发送（订单状态再次变更）
		ref.CallbackStatus = constants.CallbackStatusPending
		_ = s.refRepo.Update(ref)
	}
	if err := s.queueClient.EnqueueDownstreamCallback(queue.DownstreamCallbackPayload{
		DownstreamOrderRefID: ref.ID,
	}); err != nil {
		logger.Warnw("downstream_enqueue_callback_failed",
			"order_id", orderID,
			"ref_id", ref.ID,
			"error", err,
		)
	}
}

// callbackRequest 回调请求体
type callbackRequest struct {
	Event             string                        `json:"event"`
	OrderID           uint                          `json:"order_id"`
	OrderNo           string                        `json:"order_no"`
	DownstreamOrderNo string                        `json:"downstream_order_no"`
	Status            string                        `json:"status"`
	Fulfillment       *upstream.UpstreamFulfillment `json:"fulfillment,omitempty"`
	Timestamp         int64                         `json:"timestamp"`
}

// SendCallback 执行回调发送
func (s *DownstreamCallbackService) SendCallback(refID uint) error {
	ref, err := s.refRepo.GetByID(refID)
	if err != nil {
		return err
	}
	if ref == nil {
		return ErrDownstreamRefNotFound
	}
	if strings.TrimSpace(ref.CallbackURL) == "" {
		return nil
	}

	// 查询订单
	order, err := s.orderRepo.GetByID(ref.OrderID)
	if err != nil || order == nil {
		logger.Warnw("downstream_callback_order_not_found", "ref_id", ref.ID, "order_id", ref.OrderID)
		return err
	}

	// 查询 API 凭证获取 secret 用于签名
	credential, err := s.credentialRepo.GetByID(ref.ApiCredentialID)
	if err != nil || credential == nil {
		logger.Warnw("downstream_callback_credential_not_found", "ref_id", ref.ID, "credential_id", ref.ApiCredentialID)
		return fmt.Errorf("credential not found for ref %d", ref.ID)
	}

	// 构建回调请求
	event := "order.status_changed"
	if order.Status == constants.OrderStatusDelivered || order.Status == constants.OrderStatusCompleted {
		event = "order.fulfilled"
	}

	var fulfillment *upstream.UpstreamFulfillment
	if order.Fulfillment != nil && order.Fulfillment.Status == constants.FulfillmentStatusDelivered {
		fulfillment = &upstream.UpstreamFulfillment{
			Type:    order.Fulfillment.Type,
			Status:  order.Fulfillment.Status,
			Payload: order.Fulfillment.Payload,
		}
		if order.Fulfillment.DeliveredAt != nil {
			fulfillment.DeliveredAt = order.Fulfillment.DeliveredAt
		}
	}

	now := time.Now()
	timestamp := now.Unix()
	reqBody := callbackRequest{
		Event:             event,
		OrderID:           order.ID,
		OrderNo:           order.OrderNo,
		DownstreamOrderNo: ref.DownstreamOrderNo,
		Status:            order.Status,
		Fulfillment:       fulfillment,
		Timestamp:         timestamp,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// 签名
	signature := upstream.Sign(credential.ApiSecret, "POST", "/api/v1/upstream/callback", timestamp, bodyBytes)

	// 发送请求
	httpReq, err := http.NewRequest("POST", ref.CallbackURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(upstream.HeaderApiKey, credential.ApiKey)
	httpReq.Header.Set(upstream.HeaderTimestamp, fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set(upstream.HeaderSignature, signature)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return s.handleCallbackFailure(ref, &now, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusOK {
		var result struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(respBody, &result) == nil && result.OK {
			ref.CallbackStatus = constants.CallbackStatusSent
			ref.LastCallbackAt = &now
			return s.refRepo.Update(ref)
		}
	}

	return s.handleCallbackFailure(ref, &now, fmt.Errorf("callback returned %d: %s", resp.StatusCode, string(respBody)))
}

func (s *DownstreamCallbackService) handleCallbackFailure(ref *models.DownstreamOrderRef, now *time.Time, callbackErr error) error {
	ref.CallbackRetryCount++
	ref.LastCallbackAt = now

	maxRetries := 5
	if ref.CallbackRetryCount >= maxRetries {
		ref.CallbackStatus = constants.CallbackStatusFailed
		logger.Warnw("downstream_callback_max_retries",
			"ref_id", ref.ID,
			"order_id", ref.OrderID,
			"retry_count", ref.CallbackRetryCount,
			"error", callbackErr,
		)
	} else {
		// 递增间隔重试：30s, 60s, 120s, 300s
		delays := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second, 300 * time.Second}
		idx := ref.CallbackRetryCount - 1
		if idx >= len(delays) {
			idx = len(delays) - 1
		}
		delay := delays[idx]

		if s.queueClient != nil {
			if err := s.queueClient.EnqueueDownstreamCallback(queue.DownstreamCallbackPayload{
				DownstreamOrderRefID: ref.ID,
			}, asynq.ProcessIn(delay)); err != nil {
				logger.Warnw("downstream_callback_requeue_failed",
					"ref_id", ref.ID,
					"error", err,
				)
			}
		}
	}

	return s.refRepo.Update(ref)
}
