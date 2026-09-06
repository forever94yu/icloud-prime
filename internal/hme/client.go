// Package hme 实现了 iCloud Hide My Email 协议客户端。
//
// 基于 Cookie 会话,通过 tls-client 伪装 Chrome TLS 指纹规避 iCloud 风控。
// 对应原 Python 项目 icloud_hme.py 的 ICloudHME 类。
package hme

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"sort"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	// ClientBuildNumber 是 iCloud Web 客户端构建号,从浏览器抓包获取。
	// maildomainws (HME 别名管理) 专用。
	ClientBuildNumber = "2626Build21"
	// ClientMasteringNumber 是 iCloud Web 客户端主版本号。
	ClientMasteringNumber = "2626Build21"
	// DefaultBuildNumber 用于 validate 和 mccgateway (邮件) 等非 HME 端点。
	DefaultBuildNumber = "2626Build21"
	// RequestTimeout 单次请求超时。
	RequestTimeout = 15 * time.Second
	// MaxRetries 最大重试次数。
	MaxRetries = 3
)

var retryDelays = []time.Duration{
	1 * time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
}

// AccountInfo 是从 /validate 响应中提取的账号身份信息。
type AccountInfo struct {
	DSID             string `json:"dsid"`
	AppleID          string `json:"appleId"`
	PrimaryEmail     string `json:"primaryEmail"`
	FullName         string `json:"fullName"`
	IsManagedAppleID bool   `json:"isManagedAppleId"`
}

// Alias 是一个 Hide My Email 隐私邮箱别名。
type Alias struct {
	Email       string `json:"email"`
	AnonymousID string `json:"anonymousId"`
	Label       string `json:"label"`
	Active      bool   `json:"active"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

// Client 是 iCloud Hide My Email 客户端。
//
// 一个 Client 对应一个 iCloud 账号。通过传入的 Cookie 维持会话,
// 首次调用业务方法时会自动触发 ValidateSession 解析 HME 服务端点。
type Client struct {
	Cookies     map[string]string
	Host        string // "icloud.com" 或 "icloud.com.cn"
	Proxy       string // HTTP/SOCKS5 代理
	Username    string // iCloud 账号 (用于登录)
	Password    string // iCloud 密码 (用于登录)
	Verbose     bool
	httpc       tls_client.HttpClient
	setupURL    string
	serviceURL  string
	dsid        string // 从 validate 响应提取
	clientID    string // UUID,每次会话生成
	accountInfo *AccountInfo
}

// NewClient 创建一个新的 HME 客户端,底层使用 Chrome TLS 指纹。
//
// proxy 支持格式:
//   - HTTP:  "http://user:pass@host:port"
//   - SOCKS5: "socks5://user:pass@host:port"
func NewClient(cookies map[string]string, host, proxy string, verbose bool) (*Client, error) {
	cookies = maps.Clone(cookies)
	if cookies == nil {
		cookies = make(map[string]string)
	}
	if host == "" {
		host = "icloud.com"
	}
	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithCookieJar(jar),
		tls_client.WithNotFollowRedirects(),
	}

	// 添加代理支持
	if proxy != "" {
		options = append(options, tls_client.WithProxyUrl(proxy))
	}

	httpc, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}

	c := &Client{
		Cookies:  cookies,
		Host:     normalizeHost(host),
		Proxy:    proxy,
		Verbose:  verbose,
		httpc:    httpc,
		clientID: uuid.New().String(),
	}

	// 把传入的 Cookie 灌入 jar,后续请求自动携带。
	if len(cookies) > 0 {
		// 设置 Cookie 到所有可能的域名
		domains := []string{
			"https://www.icloud.com",
			"https://www.icloud.com.cn",
			"https://setup.icloud.com",
			"https://setup.icloud.com.cn",
			"https://" + c.Host,
		}

		// 添加 serviceURL 的域名（如果已知）
		if c.serviceURL != "" {
			if u, err := url.Parse(c.serviceURL); err == nil {
				domains = append(domains, u.Scheme+"://"+u.Host)
			}
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
	return c, nil
}

func normalizeHost(host string) string {
	h := strings.TrimSpace(strings.ToLower(host))
	if u, err := url.Parse(h); err == nil && u.Hostname() != "" {
		h = u.Hostname()
	} else if !strings.Contains(h, "://") {
		if u, err := url.Parse("https://" + h); err == nil && u.Hostname() != "" {
			h = u.Hostname()
		}
	}
	if strings.HasSuffix(h, ".icloud.com.cn") || h == "icloud.com.cn" {
		return "icloud.com.cn"
	}
	return "icloud.com"
}

// SetupURL 返回 iCloud setup 端点。
func (c *Client) SetupURL() string {
	if c.setupURL == "" {
		suffix := "setup.icloud.com"
		if c.Host == "icloud.com.cn" {
			suffix = "setup.icloud.com.cn"
		}
		c.setupURL = "https://" + suffix + "/setup/ws/1"
	}
	return c.setupURL
}

// Origin 返回 Web Origin。
func (c *Client) Origin() string {
	return "https://www." + c.Host
}

func (c *Client) log(format string, args ...any) {
	if c.Verbose {
		fmt.Printf("  [iCloud] %s\n", fmt.Sprintf(format, args...))
	}
}

// buildURL 给 URL 追加 clientBuildNumber / clientMasteringNumber / clientId / dsid 查询参数,
// 这是 iCloud Web API 的强制要求。
func (c *Client) buildURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := parsed.Query()
	// setup.icloud.com (validate) 和 mccgateway 用 DefaultBuildNumber,maildomainws 用 ClientBuildNumber
	host := parsed.Hostname()
	if strings.Contains(host, "maildomainws") {
		q.Set("clientBuildNumber", ClientBuildNumber)
		q.Set("clientMasteringNumber", ClientMasteringNumber)
	} else {
		q.Set("clientBuildNumber", DefaultBuildNumber)
		q.Set("clientMasteringNumber", DefaultBuildNumber)
	}
	if c.clientID != "" {
		q.Set("clientId", c.clientID)
	}
	if c.dsid != "" {
		q.Set("dsid", c.dsid)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// request 执行带重试的 HTTP 请求,返回响应体字符串。
func (c *Client) request(method, rawURL string, body any, timeout time.Duration, maxAttempts int) (string, error) {
	if timeout == 0 {
		timeout = RequestTimeout
	}
	if maxAttempts == 0 {
		maxAttempts = MaxRetries
	}
	fullURL := c.buildURL(rawURL)

	hostName := ""
	if u, err := url.Parse(rawURL); err == nil {
		hostName = u.Hostname()
	}
	contentType, acceptType := requestTypes(hostName)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var reqBody io.Reader
		if body != nil {
			buf, err := json.Marshal(body)
			if err != nil {
				return "", err
			}
			reqBody = bytes.NewReader(buf)
		}

		req, err := http.NewRequest(method, fullURL, reqBody)
		if err != nil {
			return "", err
		}
		req.Header.Set("Origin", c.Origin())
		req.Header.Set("Referer", c.Origin()+"/")
		req.Header.Set("Accept", acceptType)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("sec-ch-ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"Windows"`)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

		// 手动添加 Cookie 头（确保跨域也能传递）
		// 浏览器发送的 Cookie 值带双引号,iCloud 严格匹配
		if len(c.Cookies) > 0 {
			cookieParts := make([]string, 0, len(c.Cookies))
			for k, v := range c.Cookies {
				if strings.HasPrefix(v, `"`) {
					cookieParts = append(cookieParts, k+"="+v)
				} else {
					cookieParts = append(cookieParts, k+`="`+v+`"`)
				}
			}
			cookieHeader := strings.Join(cookieParts, "; ")
			req.Header.Set("Cookie", cookieHeader)
			if c.Verbose {
				c.log(">>> URL: %s", fullURL)
				c.log(">>> Cookie: %s", cookieHeader[:min(200, len(cookieHeader))])
				for k, vv := range req.Header {
					for _, v := range vv {
						c.log(">>> %s: %s", k, v[:min(100, len(v))])
					}
				}
			}
		}

		resp, err := c.httpc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("连接失败: %w", err)
			if attempt < maxAttempts {
				c.sleepRetry(attempt)
				continue
			}
			return "", lastErr
		}

		text, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// 从 Set-Cookie 响应头更新 Cookie（模拟浏览器行为,iCloud 会刷新 token）
		for _, sc := range resp.Cookies() {
			if sc.Name != "" && sc.Value != "" {
				c.Cookies[sc.Name] = sc.Value
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			snippet := string(text)
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
			// 401/403 说明 Cookie 失效,不重试直接返回。
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				return "", lastErr
			}
			if attempt < maxAttempts {
				c.sleepRetry(attempt)
				continue
			}
			return "", lastErr
		}

		return string(text), nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("未知错误")
}

func (c *Client) sleepRetry(attempt int) {
	idx := attempt - 1
	if idx >= len(retryDelays) {
		idx = len(retryDelays) - 1
	}
	time.Sleep(retryDelays[idx])
}

// ValidateSession 校验 iCloud 会话,解析 HME 服务端点和账号身份。
//
// 必须在调用 ListAliases / Generate / Reserve / Delete 之前完成。
// 失败通常意味着 Cookie 过期或未订阅 iCloud+。
func (c *Client) ValidateSession() error {
	c.log("校验 iCloud 会话...")
	c.log("使用的 Cookie 数量: %d", len(c.Cookies))
	if len(c.Cookies) > 0 {
		for k := range c.Cookies {
			c.log("Cookie: %s", k)
		}
	}

	body, err := c.request("POST", c.SetupURL()+"/validate", nil, 20*time.Second, MaxRetries)
	if err != nil {
		c.log("校验失败: %v", err)
		return err
	}
	if !gjson.Valid(body) {
		return fmt.Errorf("invalid JSON response")
	}
	data := gjson.Parse(body)
	serviceURL := data.Get("webservices.premiummailsettings.url").String()
	if serviceURL == "" {
		return fmt.Errorf(
			"iCloud 会话校验失败 — 可能原因:\n" +
				"  1. 未开通 iCloud+ 订阅 (Hide My Email 需要 iCloud+)\n" +
				"  2. Cookie 已过期,请在 Chrome 重新登录 icloud.com\n" +
				"  3. 网络问题",
		)
	}
	c.serviceURL = strings.TrimRight(serviceURL, "/")
	// 剥离 :443 端口——tls-client cookie jar 按无端口 host 存储 cookie,带端口会丢失 cookie → 401
	if strings.HasSuffix(c.serviceURL, ":443") {
		c.serviceURL = strings.TrimSuffix(c.serviceURL, ":443")
	}

	// 获取 serviceURL 后，再次设置 Cookie 到该域名
	if len(c.Cookies) > 0 {
		u, _ := url.Parse(c.serviceURL)
		httpCookies := make([]*http.Cookie, 0, len(c.Cookies))
		for k, v := range c.Cookies {
			httpCookies = append(httpCookies, &http.Cookie{
				Name:  k,
				Value: v,
				Path:  "/",
			})
		}
		c.httpc.GetCookies(u) // 触发 cookie jar 初始化
		// 注意：需要手动设置 cookie，但 tls-client 的 CookieJar 不支持直接设置
		// 我们需要在请求时手动添加 Cookie 头
	}

	dsInfo := data.Get("dsInfo")
	c.dsid = dsInfo.Get("dsid").String()
	info := &AccountInfo{
		DSID:             c.dsid,
		AppleID:          firstNonEmpty(dsInfo.Get("appleId").String(), dsInfo.Get("primaryEmail").String(), dsInfo.Get("appleIdEmail").String()),
		PrimaryEmail:     firstNonEmpty(dsInfo.Get("primaryEmail").String(), dsInfo.Get("appleId").String()),
		FullName:         firstNonEmpty(dsInfo.Get("fullName").String(), dsInfo.Get("name").String()),
		IsManagedAppleID: dsInfo.Get("isManagedAppleId").Bool(),
	}
	if info.AppleID == "" {
		for _, name := range []string{"aosappleid", "appleId", "dsid"} {
			if v, ok := c.Cookies[name]; ok && v != "" {
				info.AppleID = v
				break
			}
		}
	}
	c.accountInfo = info
	c.log("会话有效 → %s", nonEmpty(info.AppleID, "未知账号"))
	return nil
}

// AccountInfo 返回已校验的账号身份(校验前为 nil)。
func (c *Client) AccountInfo() *AccountInfo { return c.accountInfo }

func (c *Client) resolveService() error {
	if c.serviceURL == "" {
		return c.ValidateSession()
	}
	return nil
}

// ListAliases 列出当前账号所有 Hide My Email 别名。
func (c *Client) ListAliases() ([]Alias, error) {
	if err := c.resolveService(); err != nil {
		return nil, err
	}
	c.log("获取别名列表...")
	body, err := c.request("GET", c.serviceURL+"/v2/hme/list", nil, 0, MaxRetries)
	if err != nil {
		return nil, err
	}
	if !gjson.Valid(body) {
		return nil, fmt.Errorf("invalid alias list JSON response")
	}
	root := gjson.Parse(body)
	if success := root.Get("success"); success.Exists() && !success.Bool() {
		return nil, fmt.Errorf("获取别名失败: %s", nonEmpty(root.Get("error.errorMessage").String(), "unknown"))
	}
	if !root.Get("result.hmeEmails").IsArray() && !findFirstDictArray(root).IsArray() {
		return nil, fmt.Errorf("invalid alias list response: missing hmeEmails")
	}
	aliases := parseAliasList(body)
	c.log("共 %d 个别名", len(aliases))
	return aliases, nil
}

// Generate 生成一个候选别名(尚未保留,需再调用 Reserve)。
func (c *Client) Generate() (string, error) {
	if err := c.resolveService(); err != nil {
		return "", err
	}
	c.log("生成候选别名...")
	body, err := c.request("POST", c.serviceURL+"/v1/hme/generate", generatePayload(), 0, 2)
	if err != nil {
		return "", err
	}
	parsed := gjson.Parse(body)
	if !parsed.Get("success").Bool() {
		errMsg := parsed.Get("error.errorMessage").String()
		return "", fmt.Errorf("生成失败: %s", nonEmpty(errMsg, "unknown"))
	}
	hme := parsed.Get("result.hme").String()
	if hme == "" {
		// 某些响应把 hme 包在嵌套对象里
		hme = parsed.Get("result.hme.hme").String()
		if hme == "" {
			hme = parsed.Get("result.hme.email").String()
		}
	}
	c.log("候选: %s", hme)
	return hme, nil
}

// Reserve 保留/确认候选别名,使其正式生效。
func (c *Client) Reserve(hme, label string) (string, error) {
	if err := c.resolveService(); err != nil {
		return "", err
	}
	if label == "" {
		label = "Created " + time.Now().Format("2006-01-02 15:04")
	}
	c.log("保留别名 %s ...", hme)
	payload := map[string]string{
		"hme":   hme,
		"label": label,
		"note":  "Created by icloud_hme tool",
	}
	body, err := c.request("POST", c.serviceURL+"/v1/hme/reserve", payload, 0, 2)
	if err != nil {
		return "", err
	}
	parsed := gjson.Parse(body)
	if !parsed.Get("success").Bool() {
		errMsg := parsed.Get("error.errorMessage").String()
		return "", fmt.Errorf("保留失败: %s", nonEmpty(errMsg, "unknown"))
	}
	alias := hme
	resultHme := parsed.Get("result.hme")
	if resultHme.IsObject() {
		if v := resultHme.Get("hme").String(); v != "" {
			alias = v
		}
	}
	c.log("已保留: %s", alias)
	return alias, nil
}

// CreateResult 是 CreateAlias 的返回结果。
type CreateResult struct {
	Email     string `json:"email"`
	Label     string `json:"label"`
	CreatedAt string `json:"created_at"`
}

// CreateAlias 一步完成「生成 + 保留」,创建一个新别名。
//
// 由于 generate / reserve 偶发失败,内部会重试 maxRetries 次,
// 每次重试会重置 serviceURL 强制重新校验会话。
func (c *Client) CreateAlias(label string, maxRetries int) (*CreateResult, error) {
	if maxRetries <= 0 {
		maxRetries = 5
	}
	var lastErr string
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			c.serviceURL = ""
			c.setupURL = ""
			c.log("重试 %d/%d ...", attempt+1, maxRetries)
		}
		hme, err := c.Generate()
		if err != nil {
			lastErr = "generate 失败: " + err.Error()
			c.log("%s", lastErr)
			if attempt < maxRetries-1 {
				time.Sleep(time.Second)
				continue
			}
			break
		}
		email, err := c.Reserve(hme, label)
		if err != nil {
			lastErr = err.Error()
			c.log("reserve 失败: %s", lastErr)
			if attempt < maxRetries-1 {
				time.Sleep(time.Second)
				continue
			}
			break
		}
		return &CreateResult{
			Email:     email,
			Label:     label,
			CreatedAt: time.Now().Format(time.RFC3339),
		}, nil
	}
	if lastErr != "" {
		return nil, fmt.Errorf("创建别名失败: %s", lastErr)
	}
	return nil, fmt.Errorf("创建别名失败,已重试 %d 次", maxRetries)
}

// DeactivateHME 停用别名(可恢复)。
func (c *Client) DeactivateHME(anonymousID string) (bool, error) {
	if err := c.resolveService(); err != nil {
		return false, err
	}
	c.log("停用 %s ...", anonymousID)
	payload := map[string]string{"anonymousId": anonymousID}
	body, err := c.request("POST", c.serviceURL+"/v1/hme/deactivate", payload, 0, 2)
	if err != nil {
		return false, err
	}
	return gjson.Get(body, "success").Bool(), nil
}

// ReactivateHME 激活已停用的别名。
func (c *Client) ReactivateHME(anonymousID string) (bool, error) {
	if err := c.resolveService(); err != nil {
		return false, err
	}
	c.log("激活 %s ...", anonymousID)
	payload := map[string]string{"anonymousId": anonymousID}
	body, err := c.request("POST", c.serviceURL+"/v1/hme/reactivate", payload, 0, 2)
	if err != nil {
		return false, err
	}
	return gjson.Get(body, "success").Bool(), nil
}

// Delete 删除别名。若直接删除失败会先停用再删。
func (c *Client) Delete(anonymousID string) error {
	if err := c.resolveService(); err != nil {
		return err
	}
	c.log("删除 %s ...", anonymousID)
	payload := map[string]string{"anonymousId": anonymousID}
	doDelete := func() (string, error) {
		return c.request("POST", c.serviceURL+"/v1/hme/delete", payload, 0, 2)
	}
	body, err := doDelete()
	if err != nil || !gjson.Get(body, "success").Bool() {
		c.log("直接删除失败,尝试先停用...")
		_, _ = c.request("POST", c.serviceURL+"/v1/hme/deactivate", payload, 0, 2)
		body, err = doDelete()
		if err != nil {
			return err
		}
		if !gjson.Get(body, "success").Bool() {
			return fmt.Errorf("%s", gjson.Get(body, "error.errorMessage").String())
		}
	}
	c.log("已删除")
	return nil
}

// ---- 别名列表解析 (对应 ICloudHME._parse_alias_list) ----

// parseAliasList 解析 iCloud 返回的别名列表 JSON。
// 容错:优先取 result.hmeEmails,找不到则递归查找第一个对象数组。
func parseAliasList(body string) []Alias {
	if !gjson.Valid(body) {
		return []Alias{}
	}
	root := gjson.Parse(body)

	arr := root.Get("result.hmeEmails")
	if !arr.IsArray() {
		arr = findFirstDictArray(root)
	}
	if !arr.IsArray() {
		return []Alias{}
	}

	var aliases []Alias
	arr.ForEach(func(_, item gjson.Result) bool {
		if !item.IsObject() {
			return true
		}
		meta := item.Get("metaData")
		email := strings.TrimSpace(strings.ToLower(firstNonEmpty(
			item.Get("hme").String(),
			item.Get("email").String(),
			item.Get("alias").String(),
			item.Get("address").String(),
			meta.Get("hme").String(),
		)))
		if email == "" || !strings.Contains(email, "@") {
			return true
		}
		state := strings.ToLower(firstNonEmpty(item.Get("state").String(), item.Get("status").String()))
		active := state != "inactive" && state != "deleted"
		if item.Get("active").Exists() {
			active = item.Get("active").Bool() && active
		}
		if item.Get("isActive").Exists() {
			active = item.Get("isActive").Bool() && active
		}
		aliases = append(aliases, Alias{
			Email:       email,
			AnonymousID: firstNonEmpty(item.Get("anonymousId").String(), item.Get("id").String()),
			Label:       firstNonEmpty(item.Get("label").String(), meta.Get("label").String()),
			Active:      active,
			CreatedAt:   firstNonEmpty(item.Get("createTimestamp").String(), item.Get("createdAt").String()),
		})
		return true
	})

	// 活跃的排前面,再按邮箱字母序。
	sort.SliceStable(aliases, func(i, j int) bool {
		if aliases[i].Active != aliases[j].Active {
			return aliases[i].Active
		}
		return aliases[i].Email < aliases[j].Email
	})
	return aliases
}

// findFirstDictArray 递归查找第一个「对象数组」。
func findFirstDictArray(v gjson.Result) gjson.Result {
	if v.IsArray() {
		if len(v.Array()) > 0 && v.Array()[0].IsObject() {
			return v
		}
	}
	if v.IsObject() {
		var found gjson.Result
		v.ForEach(func(_, val gjson.Result) bool {
			if r := findFirstDictArray(val); r.IsArray() && len(r.Array()) > 0 {
				found = r
				return false
			}
			return true
		})
		return found
	}
	return gjson.Result{}
}

// ---- 小工具 ----

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
