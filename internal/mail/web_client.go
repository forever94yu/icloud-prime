// Package mail - iCloud Web 邮件客户端
//
// 使用 Cookie 认证通过 iCloud Web API 读取邮件，
// 无需 App Password。基于 mccgateway 服务。
package mail

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
)

// WebClientBuildNumber 是与浏览器一致的 mccgateway 邮件接口构建号。
const WebClientBuildNumber = "2626Build21"

// WebClient 是 iCloud Web 邮件客户端。
type WebClient struct {
	cookies       map[string]string
	dsid          string
	clientID      string
	mccGatewayURL string
	host          string // "icloud.com" 或 "icloud.com.cn"
	httpc         tls_client.HttpClient
}

// NewWebClient 创建一个 Web 邮件客户端。
func NewWebClient(cookies map[string]string, dsid, host string) *WebClient {
	cookies = maps.Clone(cookies)
	if cookies == nil {
		cookies = make(map[string]string)
	}
	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithCookieJar(jar),
		tls_client.WithNotFollowRedirects(),
	}

	httpc, _ := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)

	host = normalizeICloudHost(host)

	c := &WebClient{
		cookies:  cookies,
		dsid:     dsid,
		clientID: uuid.New().String(),
		host:     host,
		httpc:    httpc,
	}

	// 设置 Cookie 到所有相关域名(确保跨域请求能传递 Cookie)
	if len(cookies) > 0 {
		suffix := "icloud.com"
		if host == "icloud.com.cn" {
			suffix = "icloud.com.cn"
		}
		domains := []string{
			"https://setup." + suffix,
			"https://www." + suffix,
			"https://p217-mccgateway." + suffix,
			"https://p217-maildomainws." + suffix,
		}
		for _, domain := range domains {
			u, _ := url.Parse(domain)
			httpCookies := make([]*http.Cookie, 0, len(cookies))
			for k, v := range cookies {
				httpCookies = append(httpCookies, &http.Cookie{
					Name:  k,
					Value: v,
					Path:  "/",
				})
			}
			jar.SetCookies(u, httpCookies)
		}
	}

	return c
}

// CookiesSnapshot returns an independent copy of the current session cookies.
func (c *WebClient) CookiesSnapshot() map[string]string {
	return maps.Clone(c.cookies)
}

// origin 返回当前账号对应的 Web Origin。
func (c *WebClient) origin() string {
	return "https://www." + c.host
}

// setCommonHeaders 设置与浏览器一致的通用请求头。
func (c *WebClient) setCommonHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.origin())
	req.Header.Set("Referer", c.origin()+"/")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")
}

// withParams 给 URL 追加 clientBuildNumber / clientId / dsid 查询参数。
func (c *WebClient) withParams(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	params := u.Query()
	params.Set("clientBuildNumber", WebClientBuildNumber)
	params.Set("clientMasteringNumber", WebClientBuildNumber)
	params.Set("clientId", c.clientID)
	params.Set("dsid", c.dsid)
	u.RawQuery = params.Encode()
	return u.String()
}

func (c *WebClient) updateCookies(resp *http.Response) {
	if c.cookies == nil {
		c.cookies = make(map[string]string)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.MaxAge < 0 {
			delete(c.cookies, cookie.Name)
		} else if cookie.Value != "" {
			c.cookies[cookie.Name] = cookie.Value
		}
	}
}

// resolveMccGateway 从 validate 响应中获取 mccgateway URL。
func (c *WebClient) resolveMccGateway() error {
	if c.mccGatewayURL != "" {
		return nil
	}

	setupURL := "https://setup." + c.host + "/setup/ws/1/validate"
	req, err := http.NewRequest("POST", c.withParams(setupURL), nil)
	if err != nil {
		return err
	}
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取 validate 响应失败: %w", err)
	}
	c.updateCookies(resp)
	if resp.StatusCode != 200 {
		return fmt.Errorf("validate 失败: HTTP %d - %s", resp.StatusCode, truncate(string(body), 200))
	}

	var parsed struct {
		Webservices struct {
			Mccgateway struct {
				URL string `json:"url"`
			} `json:"mccgateway"`
		} `json:"webservices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("解析 validate 响应失败: %w", err)
	}

	mccURL := parsed.Webservices.Mccgateway.URL
	if mccURL == "" {
		return fmt.Errorf("未找到 mccgateway URL,响应: %s", truncate(string(body), 200))
	}
	if !strings.HasPrefix(mccURL, "https://") {
		mccURL = "https://" + mccURL
	}
	// 去掉端口号(如 :443)——tls-client 的 cookie jar 按不带端口的 host 存储 Cookie,
	// 带端口的 URL 会导致 Cookie 无法附加,返回 403。
	if u, err := url.Parse(mccURL); err == nil && u.Host != "" {
		u.Host = u.Hostname()
		mccURL = u.String()
	}
	c.mccGatewayURL = strings.TrimRight(mccURL, "/")
	gateway, err := url.Parse(c.mccGatewayURL)
	if err != nil {
		return fmt.Errorf("invalid mccgateway URL: %w", err)
	}
	cookies := make([]*http.Cookie, 0, len(c.cookies))
	for name, value := range c.cookies {
		cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
	}
	c.httpc.SetCookies(gateway, cookies)
	return nil
}

// threadSearchResp 是 thread/search 接口的响应结构。
type threadSearchResp struct {
	Success          *bool  `json:"success"`
	ErrorCode        string `json:"errorCode"`
	ErrorDescription string `json:"errorDescription"`
	Error            struct {
		Message string `json:"errorMessage"`
	} `json:"error"`
	TotalThreadsReturned int `json:"totalThreadsReturned"`
	ThreadList           []struct {
		ThreadID  string   `json:"threadId"`
		Subject   string   `json:"subject"`
		Senders   []string `json:"senders"`
		Preview   string   `json:"preview"`
		Timestamp int64    `json:"timestamp"`
		Flags     []string `json:"flags"`
	} `json:"threadList"`
}

// search 执行 thread/search 请求,返回解析后的邮件列表。
func (c *WebClient) search(payload any) ([]Message, error) {
	if err := c.resolveMccGateway(); err != nil {
		return nil, err
	}

	searchURL := c.withParams(c.mccGatewayURL + "/mailws2/v1/thread/search")
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", searchURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取邮件响应失败: %w", err)
	}
	c.updateCookies(resp)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("获取邮件失败: HTTP %d - %s", resp.StatusCode, truncate(string(body), 300))
	}
	var result threadSearchResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析邮件响应失败: %w", err)
	}
	if result.ErrorCode != "" || (result.Success != nil && !*result.Success) {
		return nil, fmt.Errorf("获取邮件失败: %s %s %s", result.ErrorCode, result.ErrorDescription, result.Error.Message)
	}
	if result.ThreadList == nil {
		return nil, fmt.Errorf("invalid mail response: missing threadList")
	}

	messages := make([]Message, 0, len(result.ThreadList))
	for _, t := range result.ThreadList {
		from := ""
		if len(t.Senders) > 0 {
			from = t.Senders[0]
		}
		date := ""
		if t.Timestamp > 0 {
			date = time.UnixMilli(t.Timestamp).Format(time.RFC3339)
		}
		message := Message{
			ID:      t.ThreadID,
			From:    from,
			Subject: t.Subject,
			Preview: t.Preview,
			Date:    date,
		}
		if t.Flags != nil {
			unread := !hasAttr(t.Flags, "\\Seen")
			message.Unread = &unread
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// ListInbox 列出收件箱邮件。
func (c *WebClient) ListInbox(limit int) ([]Message, error) {
	return c.search(threadSearchPayload("", "", limit))
}

// SearchMails 搜索邮件。query 为空时等价于 ListInbox。
func (c *WebClient) SearchMails(query string, limit int) ([]Message, error) {
	if query == "" {
		return c.ListInbox(limit)
	}
	return c.search(threadSearchPayload(query, "anyfield", limit))
}

// FindByAlias asks iCloud to search recipient headers, which thread digests omit.
func (c *WebClient) FindByAlias(alias string, limit int) ([]Message, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return c.ListInbox(limit)
	}
	return c.search(threadSearchPayload(alias, "recipient", limit))
}

// Request fields follow Apple's mail2 queryThreads implementation (2632Build28).
func threadSearchPayload(text, searchType string, limit int) map[string]any {
	if limit <= 0 {
		limit = 20
	}
	payload := map[string]any{
		"responseType":        "THREAD_DIGEST",
		"includeFolderStatus": true,
		"maxResults":          limit,
		"sessionHeaders": map[string]any{
			"folder": "INBOX", "modseq": nil, "threadmodseq": nil,
			"condstore": 1, "qresync": 1, "threadmode": 1,
		},
	}
	if text != "" {
		payload["searchText"] = text
		payload["searchType"] = searchType
	}
	return payload
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
