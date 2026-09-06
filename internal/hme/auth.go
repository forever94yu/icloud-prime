// Package hme - iCloud 认证模块
//
// 基于 Go-iClient 项目实现完整的 SRP (Secure Remote Password) 登录流程,
// 支持双重认证 (2FA),登录成功后提取 session token Cookie。
package hme

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"

	http "github.com/bogdanfinn/fhttp"
	"icloud-hme/internal/srp"
)

// AuthEndpoints iCloud 认证 API 端点
const (
	OAuthClientID = "d39ba9916b7251055b22c7f910e2ea796ee65e98b2ddecea8f5dde8d9d1a815d"

	authStartFmt    = "https://idmsa.apple.com/appleauth/auth/authorize/signin?frame_id=auth-%s&language=en_US&skVersion=7&iframeId=auth-%s&client_id=%s&redirect_uri=https://www.icloud.com&response_type=code&response_mode=web_message&state=auth-%s&authVersion=latest"
	authFederate    = "https://idmsa.apple.com/appleauth/auth/federate?isRememberMeEnabled=true"
	authInit        = "https://idmsa.apple.com/appleauth/auth/signin/init"
	authComplete    = "https://idmsa.apple.com/appleauth/auth/signin/complete?isRememberMeEnabled=true"
	authOptions     = "https://idmsa.apple.com/appleauth/auth"
	submitSecurity  = "https://idmsa.apple.com/appleauth/auth/verify/%s/securitycode"
	authTrust       = "https://idmsa.apple.com/appleauth/auth/2sv/trust"
	authWebFmt      = "https://setup.icloud.com/setup/ws/1/accountLogin"
	authValidateFmt = "https://setup.icloud.com/setup/ws/1/validate?clientBuildNumber=%s&clientMasteringNumber=%s&clientId=%s"
)

// OTPProvider 双重认证回调函数,返回 2FA 验证码
type OTPProvider func() (string, error)

// authState 保存认证过程中的状态
type authState struct {
	username   string
	password   string
	frameId    string
	clientId   string
	challenge  string
	authAttr   string
	sessionID  string
	scnt       string
	authToken  string
	trustToken string
	dsid       string
}

// Login 使用 iCloud 账号密码登录,获取 session token Cookie。
//
// 登录成功后,可以通过 client.GetCookies() 获取 Cookie。
// 启用 2FA 时,会调用 otpProvider 获取验证码。
func (c *Client) Login(username, password string, otpProvider OTPProvider) error {
	state := &authState{
		username: username,
		password: password,
	}

	// 1. 初始化 frameId 和 clientId
	if err := c.authStart(state); err != nil {
		return fmt.Errorf("auth start: %w", err)
	}

	// 2. 提交用户名
	if err := c.authFederate(state); err != nil {
		return fmt.Errorf("auth federate: %w", err)
	}

	// 3. SRP 协议初始化
	params := srp.GetParams(2048)
	params.NoUserNameInX = true
	srpClient := srp.NewSRPClient(params, nil)

	// 4. 获取 salt 和 B
	authInitResp, err := c.authInit(state, base64.StdEncoding.EncodeToString(srpClient.GetABytes()))
	if err != nil {
		return fmt.Errorf("auth init: %w", err)
	}
	state.challenge = authInitResp.C

	// 5. 解码 salt 和 B
	bDec, err := base64.StdEncoding.DecodeString(authInitResp.B)
	if err != nil {
		return fmt.Errorf("decode B: %w", err)
	}
	saltDec, err := base64.StdEncoding.DecodeString(authInitResp.Salt)
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}

	// 6. 生成密码密钥
	passHash := sha256.Sum256([]byte(password))
	passwordInput := passHash[:]
	if authInitResp.Protocol == "s2k_fo" {
		passwordInput = []byte(hex.EncodeToString(passwordInput))
	}
	passKey := pbkdf2.Key(passwordInput, saltDec, authInitResp.Iteration, 32, sha256.New)

	// 7. 处理挑战
	if err := srpClient.ProcessClientChanllenge([]byte(username), passKey, saltDec, bDec); err != nil {
		return fmt.Errorf("invalid SRP challenge: %w", err)
	}

	// 8. 提交 SRP 响应 (可能触发 2FA)
	if err := c.authComplete(state, base64.StdEncoding.EncodeToString(srpClient.M1), base64.StdEncoding.EncodeToString(srpClient.M2), otpProvider); err != nil {
		return fmt.Errorf("auth complete: %w", err)
	}

	// 9. 信任设备
	if err := c.getTrust(state); err != nil {
		return fmt.Errorf("get trust: %w", err)
	}

	// 10. 获取 iCloud Web 服务 Cookie
	if err := c.authenticateWeb(state); err != nil {
		return fmt.Errorf("authenticate web: %w", err)
	}

	// 11. 保存 Cookie 到 Client
	cookies := c.extractSessionCookies()
	c.Cookies = cookies
	c.log("登录成功,获取到 %d 个 Cookie", len(cookies))
	return nil
}

// --- 认证流程的各步骤 ---

// authStart 初始化 frameId 和 clientId
func (c *Client) authStart(state *authState) error {
	state.frameId = strings.ToLower(uuid.New().String())
	state.clientId = OAuthClientID

	req, err := http.NewRequest("GET", fmt.Sprintf(authStartFmt, state.frameId, state.frameId, state.clientId, state.frameId), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	state.authAttr = resp.Header.Get("X-Apple-Auth-Attributes")
	return nil
}

// authFederate 提交用户名
func (c *Client) authFederate(state *authState) error {
	data, err := json.Marshal(map[string]any{"accountName": state.username, "rememberMe": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", authFederate, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// authInitResp authInit 响应
type authInitResp struct {
	Iteration int    `json:"iteration"`
	Salt      string `json:"salt"`
	Protocol  string `json:"protocol"`
	B         string `json:"b"`
	C         string `json:"c"`
}

// authInit 初始化 SRP 认证
func (c *Client) authInit(state *authState, a string) (*authInitResp, error) {
	reqBody := map[string]interface{}{
		"a":           a,
		"accountName": state.username,
		"protocols":   []string{"s2k", "s2k_fo"},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", authInit, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth init failed: HTTP %d", resp.StatusCode)
	}
	var result authInitResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Iteration <= 0 || result.Iteration > 1000000 || result.B == "" || result.Salt == "" || result.C == "" {
		return nil, fmt.Errorf("invalid auth init response: missing or invalid SRP challenge")
	}
	if result.Protocol != "s2k" && result.Protocol != "s2k_fo" {
		return nil, fmt.Errorf("unsupported SRP protocol: %q", result.Protocol)
	}
	return &result, nil
}

// authComplete 提交 SRP 响应
func (c *Client) authComplete(state *authState, m1, m2 string, otpProvider OTPProvider) error {
	reqBody := map[string]interface{}{
		"accountName": state.username,
		"rememberMe":  true,
		"trustTokens": []string{},
		"m1":          m1,
		"c":           state.challenge,
		"m2":          m2,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", authComplete, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		return nil
	case 409:
		// 需要 2FA
		return c.handleTwoFactor(state, resp, otpProvider)
	case 403:
		return fmt.Errorf("用户名或密码错误")
	case 412:
		return fmt.Errorf("需要先在 appleid.apple.com 同意隐私条款")
	default:
		return fmt.Errorf("auth complete 失败: HTTP %d", resp.StatusCode)
	}
}

// handleTwoFactor 处理双重认证
func (c *Client) handleTwoFactor(state *authState, signinResp *http.Response, otpProvider OTPProvider) error {
	state.sessionID = signinResp.Header.Get("X-Apple-ID-Session-Id")
	state.scnt = signinResp.Header.Get("scnt")

	if otpProvider == nil {
		return fmt.Errorf("账号启用了双重认证,需要提供 OTP")
	}

	otp, err := otpProvider()
	if err != nil {
		return fmt.Errorf("获取 2FA 验证码失败: %w", err)
	}

	// 提交 2FA 验证码
	reqBody := map[string]interface{}{
		"securityCode": map[string]string{"code": otp},
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", fmt.Sprintf(submitSecurity, "trusteddevice"), bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("2FA 验证失败: HTTP %d", resp.StatusCode)
	}

	if newScnt := resp.Header.Get("scnt"); newScnt != "" {
		state.scnt = newScnt
	}
	return nil
}

// getTrust 获取 trust token
func (c *Client) getTrust(state *authState) error {
	req, err := http.NewRequest("GET", authTrust, nil)
	if err != nil {
		return err
	}

	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("trust 失败: HTTP %d", resp.StatusCode)
	}

	state.authToken = resp.Header.Get("X-Apple-Session-Token")
	state.trustToken = resp.Header.Get("X-Apple-TwoSV-Trust-Token")
	return nil
}

// authenticateWeb 认证 iCloud Web 服务
func (c *Client) authenticateWeb(state *authState) error {
	body := fmt.Sprintf(`{"dsWebAuthToken":"%s","accountCountryCode":"USA","extended_login":true,"trustToken":"%s"}`,
		state.authToken, state.trustToken)

	req, err := http.NewRequest("POST", authWebFmt, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.Origin())
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("auth web 失败: HTTP %d", resp.StatusCode)
	}

	var result struct {
		DsInfo struct {
			Dsid string `json:"dsid"`
		} `json:"dsInfo"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	state.dsid = result.DsInfo.Dsid

	// 复制 idmsa.apple.com 的 Cookie 到 icloud.com
	u1, _ := url.Parse("https://idmsa.apple.com")
	u2, _ := url.Parse("https://icloud.com")
	cookies := c.httpc.GetCookies(u1)
	c.httpc.SetCookies(u2, cookies)

	return nil
}

// extractSessionCookies 提取 session token Cookie
func (c *Client) extractSessionCookies() map[string]string {
	cookies := make(map[string]string)
	u, _ := url.Parse(c.Origin())
	for _, cookie := range c.httpc.GetCookies(u) {
		cookies[cookie.Name] = cookie.Value
	}
	return cookies
}

// updateAuthHeaders 更新认证请求所需的头部
func (c *Client) updateAuthHeaders(header http.Header, state *authState) http.Header {
	if state.scnt != "" {
		header.Set("scnt", state.scnt)
	}
	if state.sessionID != "" {
		header.Set("X-Apple-ID-Session-Id", state.sessionID)
	}

	header.Set("X-Requested-With", "XMLHttpRequest")
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("Referer", "https://idmsa.apple.com/")
	header.Set("Origin", "https://idmsa.apple.com")
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")

	return header
}

// Validate 验证当前 Cookie 是否有效
func (c *Client) Validate() (bool, error) {
	if len(c.Cookies) == 0 {
		return false, fmt.Errorf("无 Cookie")
	}
	// 简单实现：尝试调用 validate 端点
	err := c.ValidateSession()
	if err != nil {
		return false, err
	}
	return true, nil
}
