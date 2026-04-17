package public

import (
	"errors"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/dto"
	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// CZLConnectAuthorizeRequest 构建 CZL Connect 授权链接请求
type CZLConnectAuthorizeRequest struct {
	ReturnTo          string `json:"return_to" form:"return_to"`
	UpstreamProviders string `json:"upstream_providers" form:"upstream_providers"`
}

// CZLConnectAuthorize 生成授权 URL，前端获取后自行跳转
// GET/POST /api/v1/auth/czl-connect/authorize
func (h *Handler) CZLConnectAuthorize(c *gin.Context) {
	var req CZLConnectAuthorizeRequest
	if c.Request.Method == "POST" {
		if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
			shared.RespondBindError(c, err)
			return
		}
	} else {
		_ = c.ShouldBindQuery(&req)
	}

	result, err := h.UserAuthService.BuildCZLConnectAuthorizeURL(c.Request.Context(), service.CZLConnectAuthorizeRequest{
		ReturnTo:          req.ReturnTo,
		UpstreamProviders: req.UpstreamProviders,
	})
	if err != nil {
		respondCZLConnectError(c, err)
		return
	}

	response.Success(c, gin.H{
		"authorize_url": result.AuthorizeURL,
		"state":         result.State,
	})
}

// CZLConnectCallbackRequest 授权回调请求（POST 由前端携带 code/state 提交）
type CZLConnectCallbackRequest struct {
	Code  string `json:"code" form:"code" binding:"required"`
	State string `json:"state" form:"state" binding:"required"`
}

// CZLConnectCallback 处理 CZL Connect 授权回调，完成登录
// POST /api/v1/auth/czl-connect/callback
// 也可作为 redirect_uri（GET）直接承接 CZL Connect 的回跳，返回登录结果 JSON。
func (h *Handler) CZLConnectCallback(c *gin.Context) {
	var req CZLConnectCallbackRequest
	var bindErr error
	if c.Request.Method == "POST" {
		bindErr = c.ShouldBindJSON(&req)
	} else {
		bindErr = c.ShouldBindQuery(&req)
	}
	if bindErr != nil {
		h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonBadRequest, constants.LoginLogSourceCZLConnect)
		shared.RespondBindError(c, bindErr)
		return
	}

	// CZL Connect 在用户拒绝授权时会把 error 带回 redirect_uri
	if errCode := strings.TrimSpace(c.Query("error")); errCode != "" {
		h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOAuthTokenFailed, constants.LoginLogSourceCZLConnect)
		shared.RespondErrorWithMsg(c, response.CodeBadRequest, errCode, nil)
		return
	}

	result, err := h.UserAuthService.LoginWithCZLConnect(service.CZLConnectLoginInput{
		Code:    req.Code,
		State:   req.State,
		Context: c.Request.Context(),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCZLConnectDisabled):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOAuthDisabled, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_disabled", nil)
		case errors.Is(err, service.ErrCZLConnectStateInvalid):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOAuthStateInvalid, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_state_invalid", nil)
		case errors.Is(err, service.ErrCZLConnectTokenFailed):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOAuthTokenFailed, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_token_failed", err)
		case errors.Is(err, service.ErrCZLConnectUserInfoFailed):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOAuthUserinfoFail, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_userinfo_failed", err)
		case errors.Is(err, service.ErrCZLConnectEmailMissing):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOAuthUserinfoFail, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_email_missing", nil)
		case errors.Is(err, service.ErrUserDisabled):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonUserDisabled, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeUnauthorized, "error.user_disabled", nil)
		case errors.Is(err, service.ErrRegistrationDisabled):
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonBadRequest, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeForbidden, "error.registration_disabled", nil)
		default:
			h.recordUserLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonInternalError, constants.LoginLogSourceCZLConnect)
			shared.RespondError(c, response.CodeInternal, "error.login_failed", err)
		}
		return
	}

	h.recordUserLogin(c, result.User.Email, result.User.ID, constants.LoginLogStatusSuccess, "", constants.LoginLogSourceCZLConnect)
	payload := gin.H{
		"user":       dto.NewUserAuthBriefResp(result.User),
		"token":      result.Token,
		"expires_at": result.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		"return_to":  result.ReturnTo,
		"oauth": gin.H{
			"access_token":  result.AccessToken,
			"refresh_token": result.RefreshToken,
			"expires_at":    formatOAuthExpiry(result.AccessExpiry),
		},
	}
	response.Success(c, payload)
}

// CZLConnectRefreshRequest 刷新 CZL Connect 访问令牌请求
type CZLConnectRefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// CZLConnectRefresh 使用 refresh_token 刷新 CZL Connect 访问令牌
// POST /api/v1/auth/czl-connect/refresh
func (h *Handler) CZLConnectRefresh(c *gin.Context) {
	var req CZLConnectRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}
	token, err := h.UserAuthService.RefreshCZLConnectToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		respondCZLConnectError(c, err)
		return
	}
	response.Success(c, gin.H{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_in":    token.ExpiresIn,
		"scope":         token.Scope,
	})
}

// respondCZLConnectError 统一映射 CZL Connect 通用错误
func respondCZLConnectError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCZLConnectDisabled):
		shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_disabled", nil)
	case errors.Is(err, service.ErrCZLConnectStateInvalid):
		shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_state_invalid", nil)
	case errors.Is(err, service.ErrCZLConnectTokenInvalid):
		shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_token_invalid", nil)
	case errors.Is(err, service.ErrCZLConnectTokenFailed):
		shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_token_failed", err)
	case errors.Is(err, service.ErrCZLConnectUserInfoFailed):
		shared.RespondError(c, response.CodeBadRequest, "error.czl_connect_userinfo_failed", err)
	default:
		shared.RespondError(c, response.CodeInternal, "error.login_failed", err)
	}
}

// formatOAuthExpiry 格式化访问令牌到期时间，零值返回空串
func formatOAuthExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05Z07:00")
}
