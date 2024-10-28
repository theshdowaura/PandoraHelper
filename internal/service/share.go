package service

import (
	v1 "PandoraHelper/api/v1"
	"PandoraHelper/internal/model"
	"PandoraHelper/internal/repository"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"strconv"
	"strings"
	"time"
)

type ShareService interface {
	GetShareTokenByAccessToken(ctx context.Context, accessToken string, share *model.Share, resetLimit bool) (string, error)
	RefreshShareToken(ctx context.Context, share *model.Share, accessToken string, resetLimit bool) (string, error)
	ResetShareLimit(ctx context.Context, id int64) error
	GetShare(ctx context.Context, id int64) (*model.Share, error)
	Update(ctx context.Context, share *model.Share) error
	Create(ctx context.Context, share *model.Share) error
	SearchShare(ctx context.Context, accountType string, email string, uniqueName string) ([]*model.Share, error)
	DeleteShare(ctx context.Context, id int64) error
	LoginShareByPassword(ctx context.Context, username string, password string) (string, error)
	GetSharesByAccountId(ctx context.Context, accountId int) ([]*model.Share, error)
	GetOauthLoginUrl(ctx context.Context, share *model.Share) (string, error)
	ShareResetPassword(ctx context.Context, uniqueName string, password string, newPassword string, confirmNewPassword string) error
	ShareStatistic(ctx context.Context, accountId int) (interface{}, interface{})
}

func NewShareService(service *Service, shareRepository repository.ShareRepository, viper *viper.Viper, coordinator *Coordinator) ShareService {
	return &shareService{
		Service:         service,
		shareRepository: shareRepository,
		viper:           viper,
		coordinator:     coordinator,
	}
}

type shareService struct {
	*Service
	shareRepository repository.ShareRepository
	viper           *viper.Viper
	coordinator     *Coordinator
}

func (s *shareService) SearchShare(context.Context, string, string, string) (
	[]*model.Share,
	error,
) {
	//TODO implement me
	panic("implement me")
}

func (s *shareService) GetOauthLoginUrl(ctx context.Context, share *model.Share) (string, error) {
	url := ""
	if share.ShareType == "" || share.ShareType == "chatgpt" {
		loginUrl, err := s.GetChatGPTOauthLoginUrl(share)
		if err != nil {
			return "", err
		}
		url = loginUrl
	} else if share.ShareType == "claude" {
		expiresIn := 0
		if share.ExpiresAt != "" {
			expiresAt := share.ExpiresAt + " 23:59:59"
			// 把expires_at 转为unix时间戳
			expiresAtTime, err := time.Parse("2006-01-02 15:04:05", expiresAt)
			if err != nil {
				return "", err
			}
			expiresAtUnix := expiresAtTime.Unix()
			// 如果过期时间小于当前时间，返回错误
			if expiresAtUnix < time.Now().Unix() {
				return "", fmt.Errorf("share token expired")
			}
			// 获取相隔的秒数
			expiresIn = int(expiresAtUnix - time.Now().Unix())
		}
		share.ExpiresIn = expiresIn
		loginUrl, err := s.GetClaudeOauthLoginUrl(ctx, share)
		if err != nil {
			return "", err
		}
		url = loginUrl
	}
	return url, nil
}

func (s *shareService) GetChatGPTOauthLoginUrl(share *model.Share) (string, error) {
	indexDomain := fmt.Sprintf("%s/api/auth/oauth_token", s.viper.GetString("pandora.domain.index"))
	reqBody := map[string]interface{}{
		"share_token": share.ShareToken,
	}
	result := struct {
		LoginUrl   string `json:"login_url"`
		OauthToken string `json:"oauth_token"`
	}{}
	client := resty.New()
	client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: false}) // 确保启用 TLS 验证

	_, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Origin", s.viper.GetString("pandora.domain.index")).
		SetHeader("X-Forwarded-Proto", "https"). // 指定协议为 HTTPS
		SetHeader("Cache-Control", "no-cache").  // 防止 CDN 缓存
		SetBody(reqBody).
		SetResult(&result).
		Post(indexDomain)
	if err != nil {
		s.logger.Error("LoginShareByPassword error", zap.Any("err", err))
		return "", err
	}
	s.logger.Info("LoginShareByPassword resp", zap.Any("resp", result))
	finalLoginUrl := fmt.Sprintf("%s/auth/login_oauth?token=%s", s.viper.GetString("pandora.domain.index"), result.OauthToken)
	return finalLoginUrl, nil
}

func (s *shareService) GetClaudeOauthLoginUrl(ctx context.Context, share *model.Share) (string, error) {
	account, err := s.coordinator.AccountSvc.GetAccount(ctx, int64(share.AccountID))
	if err != nil {
		return "", err
	}
	if account.SessionKey == "" {
		return "", fmt.Errorf("session key is empty")
	}
	indexDomain := fmt.Sprintf("%s/manage-api/auth/oauth_token", s.viper.GetString("pandora.domain.claude"))
	reqBody := map[string]interface{}{
		"session_key": account.SessionKey,
		"unique_name": share.UniqueName,
		"expires_in":  share.ExpiresIn,
	}
	result := struct {
		ExpiresAt  int64  `json:"expires_at"`
		LoginUrl   string `json:"login_url"`
		OauthToken string `json:"oauth_token"`
	}{}
	client := resty.New()
	client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: false}) // 确保启用 TLS 验证

	_, err = client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Origin", s.viper.GetString("pandora.domain.claude")).
		SetHeader("X-Forwarded-Proto", "https"). // 指定协议为 HTTPS
		SetHeader("Cache-Control", "no-cache").  // 防止 CDN 缓存
		SetBody(reqBody).
		SetResult(&result).
		Post(indexDomain)
	if err != nil {
		s.logger.Error("LoginShareByPassword error", zap.Any("err", err))
		return "", err
	}
	s.logger.Info("LoginShareByPassword resp", zap.Any("resp", result))
	url := fmt.Sprintf("%s%s", s.viper.GetString("pandora.domain.claude"), result.LoginUrl)
	return url, nil
}

func (s *shareService) GetShareTokenByAccessToken(_ context.Context, accessToken string, share *model.Share, resetLimit bool) (string, error) {
	chatDomain := fmt.Sprintf("%s/token/register", s.viper.GetString("pandora.domain.chat"))
	var resp struct {
		TokenKey string `json:"token_key"`
	}
	client := resty.New()
	oresp, err := client.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormData(map[string]string{
			"unique_name":        share.UniqueName,
			"access_token":       accessToken,
			"expires_in":         fmt.Sprintf("%d", share.ExpiresIn),
			"site_limit":         share.SiteLimit,
			"reset_limit":        fmt.Sprintf("%t", resetLimit),
			"show_conversations": fmt.Sprintf("%t", !share.ShowConversations),
			"show_userinfo":      fmt.Sprintf("%t", share.ShowUserinfo),
			"temporary_chat":     fmt.Sprintf("%t", share.TemporaryChat),
			"gpt4o_limit":        fmt.Sprintf("%d", share.Gpt4oLimit),
			"gpt4_limit":         fmt.Sprintf("%d", share.Gpt4Limit),
			"gpt4o_mini_limit":   fmt.Sprintf("%d", share.Gpt4oMiniLimit),
			"o1_limit":           fmt.Sprintf("%d", share.O1Limit),
			"o1_mini_limit":      fmt.Sprintf("%d", share.O1MiniLimit),
		}).
		SetResult(&resp).
		Post(chatDomain)
	// 如果code不为200，返回body中的错误信息
	switch {
	case oresp.StatusCode() != 200:
		s.logger.Error("RefreshShareToken error", zap.Any("err", oresp))
		return "", fmt.Errorf(oresp.String())
	}
	if err != nil {
		s.logger.Error("RefreshShareToken error", zap.Any("err", err))
		return "", err
	}
	s.logger.Info("RefreshShareToken resp", zap.Any("resp", resp))
	return resp.TokenKey, nil
}

func (s *shareService) GetSharesByAccountId(ctx context.Context, accountId int) ([]*model.Share, error) {
	return s.shareRepository.GetSharesByAccountId(ctx, accountId)
}

func (s *shareService) Update(ctx context.Context, share *model.Share) error {
	if share.ShareType == "" || share.ShareType == "chatgpt" {
		shareToken, err := s.RefreshShareToken(ctx, share, "", false)
		if err != nil {
			return err
		}
		share.ShareToken = shareToken
	}
	err := s.shareRepository.Update(ctx, share)
	if err != nil {
		return err
	}
	return nil
}

func (s *shareService) RefreshShareToken(ctx context.Context, share *model.Share, accessToken string, resetLimit bool) (string, error) {
	if share.ShareType != "" && share.ShareType != "chatgpt" {
		return "", fmt.Errorf("share type error")
	}
	if accessToken == "" {
		account, err := s.coordinator.AccountSvc.GetAccount(ctx, int64(share.AccountID))
		if err != nil {
			return "", err
		}
		accessToken = account.AccessToken
	}
	// 判断ExpiresAt YYYY-MM-DD 23:59:59
	if share.ExpiresIn != -1 && share.ExpiresAt != "" {
		atExp, err := s.jwt.ParseTokenExp(accessToken)
		if err != nil {
			return "", err
		}
		now := time.Now().Unix()
		shareExp, err := time.Parse("2006-01-02 15:04:05", share.ExpiresAt+" 23:59:59")
		if err != nil {
			return "", err
		}
		shareExpUnix := shareExp.Unix()
		// 如果 过期日期 大于 AccessToken的过期日期，则将ExpiresIn设置0
		if shareExpUnix > atExp {
			share.ExpiresIn = 0
		} else if shareExpUnix > now {
			// 过期时间大于当前时间，小于AccessToken的过期时间，设置ExpiresIn
			share.ExpiresIn = int(shareExpUnix - now)
		} else {
			// 过期时间小于当前时间，已过期
			// 如果备注为[已过期]开头，则不再添加
			if strings.HasPrefix(share.Comment, "[已过期]") {
				return "", nil
			}
			share.ExpiresIn = -1
			share.Comment = "[已过期]" + share.Comment
			err := s.shareRepository.Update(ctx, share)
			if err != nil {
				return "", err
			}
			return "", nil
		}
	}

	return s.GetShareTokenByAccessToken(ctx, accessToken, share, resetLimit)
}

func (s *shareService) Create(ctx context.Context, share *model.Share) error {
	if share.ShareType == "" || share.ShareType == "chatgpt" {
		token, err := s.RefreshShareToken(ctx, share, "", false)
		if err != nil {
			return err
		}
		share.ShareToken = token
	}
	err := s.shareRepository.Create(ctx, share)
	if err != nil {
		return err
	}
	return nil
}

func (s *shareService) DeleteShare(ctx context.Context, id int64) error {
	share, err := s.GetShare(ctx, id)
	if err != nil {
		return err
	}
	share.ExpiresIn = -1
	if share.ShareType == "" || share.ShareType == "chatgpt" {
		_, err = s.RefreshShareToken(ctx, share, "", false)
		if err != nil {
			return err
		}

	}
	return s.shareRepository.DeleteShare(ctx, id)
}

func (s *shareService) GetShare(ctx context.Context, id int64) (*model.Share, error) {
	return s.shareRepository.GetShare(ctx, id)
}

func (s *shareService) ResetShareLimit(ctx context.Context, id int64) error {
	share, err := s.GetShare(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.RefreshShareToken(ctx, share, "", true)
	if err != nil {
		return err
	}
	return nil
}

func (s *shareService) LoginShareByPassword(ctx context.Context, username string, password string) (string, error) {
	share, err := s.shareRepository.GetShareByUniqueName(ctx, username)
	if err != nil {
		return "", err
	}
	if share.Password != password {
		return "", v1.ErrUsernameOrPassword
	}
	url, err := s.GetOauthLoginUrl(ctx, share)
	if err != nil {
		return "", err
	}
	return url, nil
}

func (s *shareService) ShareResetPassword(ctx context.Context, uniqueName string, password string, newPassword string, confirmNewPassword string) error {
	share, err := s.shareRepository.GetShareByUniqueName(ctx, uniqueName)
	if err != nil {
		return err
	}
	if share.Password != password {
		return v1.ErrUsernameOrPassword
	}
	if newPassword != confirmNewPassword {
		return v1.ErrPasswordNotMatch
	}
	share.Password = newPassword
	err = s.shareRepository.Update(ctx, share)
	if err != nil {
		return err
	}
	return nil
}

func (s *shareService) ShareStatistic(ctx context.Context, accountId int) (interface{}, interface{}) {
	account, err := s.coordinator.AccountSvc.GetAccount(ctx, int64(accountId))
	if err != nil {
		return nil, err
	}
	shares := account.Shares

	uniqueNames := make([]string, 0)
	modelTypes := make(map[string]bool)
	shareData := make(map[string]v1.Usage)

	for _, share := range shares {
		uniqueNames = append(uniqueNames, share.UniqueName)
		result, err := s.GetShareTokenInfo(share.ShareToken, account.AccessToken)
		if err != nil {
			return nil, nil
		}
		shareData[share.UniqueName] = result.Usage

		for modelName := range result.Usage {
			if modelName != "range" {
				modelTypes[modelName] = true
			}
		}
	}

	series := make([]map[string]interface{}, 0)
	for modelName := range modelTypes {
		seriesItem := map[string]interface{}{
			"name": modelName,
			"data": make([]int, len(uniqueNames)),
		}
		for i, uniqueName := range uniqueNames {
			usage := shareData[uniqueName]
			if value, exists := usage[modelName]; exists {
				if intValue, err := strconv.Atoi(value); err == nil {
					seriesItem["data"].([]int)[i] = intValue
				}
			}
		}
		series = append(series, seriesItem)
	}

	return map[string]interface{}{
		"categories": uniqueNames,
		"series":     series,
	}, nil
}

func (s *shareService) GetShareTokenInfo(shareToken string, accessToken string) (v1.StatisticResult, error) {
	host := fmt.Sprintf("%s/token/info/%s", s.viper.GetString("pandora.domain.chat"), shareToken)
	headers := map[string]string{}
	if accessToken != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", accessToken)
	}
	client := resty.New()
	var result v1.StatisticResult
	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeaders(headers).
		SetResult(&result).
		Get(host)

	if err != nil {
		s.logger.Error("GetShareTokenInfo error", zap.Any("err", err))
		return result, err
	}

	s.logger.Info("GetShareTokenInfo resp", zap.Any("resp", resp))
	return result, nil
}
