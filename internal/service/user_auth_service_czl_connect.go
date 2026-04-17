package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// CZLConnectLoginInput CZL Connect 登录输入
type CZLConnectLoginInput struct {
	Code    string
	State   string
	Context context.Context
}

// CZLConnectLoginResult CZL Connect 登录结果
type CZLConnectLoginResult struct {
	User         *models.User
	Token        string
	ExpiresAt    time.Time
	RefreshToken string
	AccessToken  string
	AccessExpiry time.Time
	ReturnTo     string
}

// LoginWithCZLConnect 使用授权码完成 CZL Connect 登录（授权码换 token → userinfo → 映射本站用户）
func (s *UserAuthService) LoginWithCZLConnect(input CZLConnectLoginInput) (*CZLConnectLoginResult, error) {
	if s.czlConnectService == nil || !s.czlConnectService.Enabled() {
		return nil, ErrCZLConnectDisabled
	}
	if s.userOAuthIdentityRepo == nil || s.userRepo == nil {
		return nil, ErrCZLConnectDisabled
	}

	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}

	callback, err := s.czlConnectService.HandleCallback(ctx, CZLConnectCallbackInput{
		Code:  input.Code,
		State: input.State,
	})
	if err != nil {
		return nil, err
	}

	user, err := s.upsertCZLConnectUser(&callback.UserInfo)
	if err != nil {
		return nil, err
	}

	token, expiresAt, err := s.GenerateUserJWT(user, 0)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	if err := s.userRepo.Update(user); err != nil {
		return nil, err
	}
	_ = cache.SetUserAuthState(context.Background(), cache.BuildUserAuthState(user))

	accessExpiry := time.Time{}
	if callback.Token.ExpiresIn > 0 {
		accessExpiry = now.Add(time.Duration(callback.Token.ExpiresIn) * time.Second)
	}

	return &CZLConnectLoginResult{
		User:         user,
		Token:        token,
		ExpiresAt:    expiresAt,
		AccessToken:  callback.Token.AccessToken,
		RefreshToken: callback.Token.RefreshToken,
		AccessExpiry: accessExpiry,
		ReturnTo:     callback.ReturnTo,
	}, nil
}

// upsertCZLConnectUser 按身份映射查找或创建本站用户，并同步最新的第三方资料
func (s *UserAuthService) upsertCZLConnectUser(info *CZLConnectUserInfo) (*models.User, error) {
	providerUserID := strconv.FormatInt(info.ID, 10)
	identity, err := s.userOAuthIdentityRepo.GetByProviderUserID(constants.UserOAuthProviderCZLConnect, providerUserID)
	if err != nil {
		return nil, err
	}

	if identity != nil {
		user, err := s.getActiveUserByID(identity.UserID)
		if err != nil {
			return nil, err
		}
		if applyCZLConnectIdentity(info, identity) {
			identity.UpdatedAt = time.Now()
			if err := s.userOAuthIdentityRepo.Update(identity); err != nil {
				return nil, err
			}
		}
		return user, nil
	}

	user, err := s.findOrCreateCZLConnectUser(info)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	authAt := now
	newIdentity := &models.UserOAuthIdentity{
		UserID:         user.ID,
		Provider:       constants.UserOAuthProviderCZLConnect,
		ProviderUserID: providerUserID,
		Username:       czlConnectPickUsername(info),
		AvatarURL:      strings.TrimSpace(info.Avatar),
		AuthAt:         &authAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.userOAuthIdentityRepo.Create(newIdentity); err != nil {
		existing, getErr := s.userOAuthIdentityRepo.GetByProviderUserID(constants.UserOAuthProviderCZLConnect, providerUserID)
		if getErr != nil || existing == nil {
			return nil, err
		}
		return s.getActiveUserByID(existing.UserID)
	}
	return user, nil
}

// findOrCreateCZLConnectUser 按邮箱查找或自动注册本站账号
func (s *UserAuthService) findOrCreateCZLConnectUser(info *CZLConnectUserInfo) (*models.User, error) {
	email := strings.ToLower(strings.TrimSpace(info.Email))
	if email == "" {
		return nil, ErrCZLConnectEmailMissing
	}

	user, err := s.userRepo.GetByEmail(email)
	if err != nil {
		return nil, err
	}
	if user != nil {
		if strings.ToLower(strings.TrimSpace(user.Status)) != constants.UserStatusActive {
			return nil, ErrUserDisabled
		}
		return user, nil
	}

	if s.settingService != nil {
		registrationEnabled, err := s.settingService.GetRegistrationEnabled(true)
		if err != nil {
			return nil, err
		}
		if !registrationEnabled {
			return nil, ErrRegistrationDisabled
		}
	}

	randomSuffix, err := randomNumericCode(16)
	if err != nil {
		return nil, err
	}
	passwordSeed := fmt.Sprintf("czl_%d_%s", info.ID, randomSuffix)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(passwordSeed), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	display := strings.TrimSpace(info.Nickname)
	if display == "" {
		display = czlConnectPickUsername(info)
	}
	if display == "" {
		display = resolveNicknameFromEmail(email)
	}
	user = &models.User{
		Email:                 email,
		PasswordHash:          string(hashedPassword),
		PasswordSetupRequired: true,
		DisplayName:           display,
		Status:                constants.UserStatusActive,
		EmailVerifiedAt:       &now,
		LastLoginAt:           &now,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.userRepo.Create(user); err != nil {
		return nil, err
	}
	if s.memberLevelSvc != nil {
		_ = s.memberLevelSvc.AssignDefaultLevel(user.ID)
	}
	return user, nil
}

// RefreshCZLConnectToken 暴露刷新令牌能力给上层调用（handler 层使用）
func (s *UserAuthService) RefreshCZLConnectToken(ctx context.Context, refreshToken string) (*CZLConnectTokenResult, error) {
	if s.czlConnectService == nil {
		return nil, ErrCZLConnectDisabled
	}
	return s.czlConnectService.RefreshToken(ctx, refreshToken)
}

// BuildCZLConnectAuthorizeURL 转发授权 URL 生成请求，便于 handler 直接调用
func (s *UserAuthService) BuildCZLConnectAuthorizeURL(ctx context.Context, req CZLConnectAuthorizeRequest) (*CZLConnectAuthorizeResult, error) {
	if s.czlConnectService == nil {
		return nil, ErrCZLConnectDisabled
	}
	return s.czlConnectService.BuildAuthorizeURL(ctx, req)
}

// applyCZLConnectIdentity 同步上游返回的最新资料到本地身份记录
// 每次登录都会刷新 AuthAt，因此总是返回需要持久化。
func applyCZLConnectIdentity(info *CZLConnectUserInfo, identity *models.UserOAuthIdentity) bool {
	if info == nil || identity == nil {
		return false
	}
	username := czlConnectPickUsername(info)
	if identity.Username != username {
		identity.Username = username
	}
	avatar := strings.TrimSpace(info.Avatar)
	if identity.AvatarURL != avatar {
		identity.AvatarURL = avatar
	}
	now := time.Now()
	identity.AuthAt = &now
	return true
}

// czlConnectPickUsername 取 username，退化到 nickname / email 本地部分
func czlConnectPickUsername(info *CZLConnectUserInfo) string {
	if info == nil {
		return ""
	}
	if v := strings.TrimSpace(info.Username); v != "" {
		return v
	}
	if v := strings.TrimSpace(info.Nickname); v != "" {
		return v
	}
	email := strings.TrimSpace(info.Email)
	if email == "" {
		return ""
	}
	return resolveNicknameFromEmail(strings.ToLower(email))
}
