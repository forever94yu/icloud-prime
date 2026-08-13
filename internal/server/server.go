// Package server 提供 HTTP API,基于 Gin。
//
// 两个核心接口:
//
//	POST /api/create  — 在指定账号下创建一个 Hide My Email 别名
//	GET  /api/inbox   — 读取指定账号(或指定别名)收到的邮件
//
// 辅助接口(用于多账号管理):账号增删查、别名列表、设置 App 密码。
package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/account"
	"icloud-hme/internal/createjob"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
)

// Server 封装 Gin 引擎和账号管理器。
type Server struct {
	mgr        *account.Manager
	scheduler  *createjob.Scheduler
	r          *gin.Engine
	cache      *responseCache
	cacheTTL   time.Duration
	folderTTL  time.Duration
	messageTTL time.Duration
}

type responseCache struct {
	mu        sync.RWMutex
	aliases   map[string]aliasCacheEntry
	mailboxes map[string]mailboxCacheEntry
	messages  map[string]messageCacheEntry
}

type aliasCacheEntry struct {
	expires time.Time
	data    []hme.Alias
}

type mailboxCacheEntry struct {
	expires time.Time
	data    []mail.Folder
}

type messageCacheEntry struct {
	expires time.Time
	data    *mail.FullMessage
}

// New 创建 Server。debug 为 true 时启用 Gin 调试日志。
func New(mgr *account.Manager, debug bool, dataDir ...string) *Server {
	dir := "."
	if len(dataDir) > 0 && dataDir[0] != "" {
		dir = dataDir[0]
	}
	scheduler, err := createjob.NewScheduler(createjob.Config{
		StorePath: filepath.Join(dir, "create_jobs.json"),
		Creator:   hmeAliasCreator{mgr: mgr},
	})
	if err != nil {
		panic(err)
	}
	srv := NewWithScheduler(mgr, scheduler, debug)
	scheduler.Start(context.Background(), time.Minute)
	return srv
}

func NewWithScheduler(mgr *account.Manager, scheduler *createjob.Scheduler, debug bool) *Server {
	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}
	s := &Server{
		mgr:       mgr,
		scheduler: scheduler,
		cache: &responseCache{
			aliases:   make(map[string]aliasCacheEntry),
			mailboxes: make(map[string]mailboxCacheEntry),
			messages:  make(map[string]messageCacheEntry),
		},
		cacheTTL:   3 * time.Minute,
		folderTTL:  10 * time.Minute,
		messageTTL: 10 * time.Minute,
	}
	s.r = gin.Default() // 自带 Logger + Recovery 中间件
	s.register()
	s.registerStatic()
	return s
}

// Run 启动 HTTP 服务。
func (s *Server) Run(addr string) error {
	return s.r.Run(addr)
}

// Handler 返回底层 gin 引擎(便于测试)。
func (s *Server) Handler() http.Handler { return s.r }

func (s *Server) register() {
	api := s.r.Group("/api")
	{
		// ===== 账号管理 =====
		api.GET("/accounts", s.listAccounts)
		api.POST("/accounts", s.addAccount)
		api.DELETE("/accounts/:id", s.removeAccount)
		api.POST("/accounts/:id/password", s.setAppPassword)
		api.PUT("/accounts/:id/cookies", s.updateCookies)
		api.POST("/accounts/:id/login", s.loginAccount)

		// ===== 核心接口 1: 创建邮箱 =====
		api.POST("/create", s.createAlias)
		api.POST("/create/batch", s.createAliasBatch)
		api.GET("/create/jobs", s.listCreateJobs)
		api.POST("/create/jobs", s.upsertCreateJob)
		api.POST("/create/jobs/:id/pause", s.pauseCreateJob)
		api.POST("/create/jobs/:id/resume", s.resumeCreateJob)
		api.DELETE("/create/jobs/:id", s.deleteCreateJob)

		// ===== 核心接口 2: 读取邮件 =====
		api.GET("/inbox", s.listInbox)
		api.GET("/messages/:id", s.getMessage)
		api.POST("/messages", s.getMessages)
		api.GET("/mailboxes", s.listMailboxes)

		// ===== 别名管理 =====
		api.GET("/aliases", s.listAliases)
		api.POST("/aliases/:id/deactivate", s.deactivateAlias)
		api.POST("/aliases/:id/reactivate", s.reactivateAlias)
		api.DELETE("/aliases/:id", s.deleteAlias)

		// ===== 系统 =====
		api.POST("/reload", s.reloadConfig)
	}
}

func (s *Server) cachedAliases(accountID string) ([]hme.Alias, bool) {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	entry, ok := s.cache.aliases[accountID]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return append([]hme.Alias(nil), entry.data...), true
}

func (s *Server) setCachedAliases(accountID string, aliases []hme.Alias) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.aliases[accountID] = aliasCacheEntry{
		expires: time.Now().Add(s.cacheTTL),
		data:    append([]hme.Alias(nil), aliases...),
	}
}

func (s *Server) invalidateAliases(accountID string) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	delete(s.cache.aliases, accountID)
}

func (s *Server) cachedMailboxes(accountID string) ([]mail.Folder, bool) {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	entry, ok := s.cache.mailboxes[accountID]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return append([]mail.Folder(nil), entry.data...), true
}

func (s *Server) setCachedMailboxes(accountID string, folders []mail.Folder) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.mailboxes[accountID] = mailboxCacheEntry{
		expires: time.Now().Add(s.folderTTL),
		data:    append([]mail.Folder(nil), folders...),
	}
}

func messageCacheKey(accountID, folder string, uid uint32) string {
	return accountID + "|" + folder + "|" + strconv.FormatUint(uint64(uid), 10)
}

func cloneFullMessage(message *mail.FullMessage) *mail.FullMessage {
	if message == nil {
		return nil
	}
	clone := *message
	return &clone
}

func (s *Server) cachedMessage(accountID, folder string, uid uint32) (*mail.FullMessage, bool) {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	entry, ok := s.cache.messages[messageCacheKey(accountID, folder, uid)]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return cloneFullMessage(entry.data), true
}

func (s *Server) setCachedMessage(accountID, folder string, uid uint32, message *mail.FullMessage) {
	if message == nil {
		return
	}
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.messages[messageCacheKey(accountID, folder, uid)] = messageCacheEntry{
		expires: time.Now().Add(s.messageTTL),
		data:    cloneFullMessage(message),
	}
}

func (s *Server) clearCache() {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.aliases = make(map[string]aliasCacheEntry)
	s.cache.mailboxes = make(map[string]mailboxCacheEntry)
	s.cache.messages = make(map[string]messageCacheEntry)
}

// ---- 统一响应 ----

type apiResp struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, apiResp{Success: true, Data: data})
}

func fail(c *gin.Context, code int, msg string) {
	c.JSON(code, apiResp{Success: false, Message: msg})
}

type hmeAliasCreator struct {
	mgr *account.Manager
}

func (h hmeAliasCreator) CreateAlias(ctx context.Context, accountID, label string) (*createjob.CreateResult, error) {
	_ = ctx
	client, err := h.mgr.HMEClient(accountID, false)
	if err != nil {
		return nil, err
	}
	result, err := client.CreateAlias(label, 5)
	_ = h.mgr.SaveCookies(accountID, client.Cookies)
	if err != nil {
		return nil, err
	}
	return &createjob.CreateResult{
		Email:     result.Email,
		Label:     result.Label,
		CreatedAt: result.CreatedAt,
		AccountID: accountID,
	}, nil
}

// ====================================================================
// 核心接口 1: 创建邮箱
//   POST /api/create
//   body: {"account_id": "acc_xxx", "label": "可选标签"}
//   返回: 新创建的 HME 邮箱地址
// ====================================================================

type createReq struct {
	AccountID string `json:"account_id" binding:"required"`
	Label     string `json:"label"`
}

func (s *Server) createAlias(c *gin.Context) {
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: account_id 必填 — "+err.Error())
		return
	}

	result, err := s.scheduler.CreateOne(c.Request.Context(), req.AccountID, req.Label)
	if err != nil {
		// 区分会话失效(需重新登录)与临时失败
		msg := err.Error()
		if errors.Is(err, createjob.ErrHourlyQuotaExceeded) {
			fail(c, http.StatusTooManyRequests, msg)
		} else if isSessionError(msg) {
			fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+msg)
		} else {
			fail(c, http.StatusBadGateway, "创建邮箱失败: "+msg)
		}
		return
	}

	ok(c, gin.H{
		"email":      result.Email,
		"label":      result.Label,
		"created_at": result.CreatedAt,
		"account_id": req.AccountID,
	})
	s.invalidateAliases(req.AccountID)
}

func (s *Server) createAliasBatch(c *gin.Context) {
	var req createjob.BatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: account_id, count 必填 — "+err.Error())
		return
	}
	resp, err := s.scheduler.BatchCreate(c.Request.Context(), req)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "count") || strings.Contains(msg, "account_id") {
			fail(c, http.StatusBadRequest, msg)
		} else if isSessionError(msg) {
			fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+msg)
		} else {
			fail(c, http.StatusBadGateway, "批量创建失败: "+msg)
		}
		return
	}
	if resp.CreatedCount > 0 {
		s.invalidateAliases(req.AccountID)
	}
	ok(c, resp)
}

func (s *Server) listCreateJobs(c *gin.Context) {
	accountID := strings.TrimSpace(c.Query("account_id"))
	data := gin.H{
		"jobs": s.scheduler.ListJobs(accountID),
	}
	if accountID != "" {
		data["remaining_this_hour"] = s.scheduler.RemainingThisHour(accountID)
	}
	ok(c, data)
}

func (s *Server) upsertCreateJob(c *gin.Context) {
	var req createjob.JobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	job, err := s.scheduler.UpsertJob(req)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, job)
}

func (s *Server) pauseCreateJob(c *gin.Context) {
	job, err := s.scheduler.PauseJob(c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	ok(c, job)
}

func (s *Server) resumeCreateJob(c *gin.Context) {
	job, err := s.scheduler.ResumeJob(c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	ok(c, job)
}

func (s *Server) deleteCreateJob(c *gin.Context) {
	id := c.Param("id")
	if err := s.scheduler.DeleteJob(id); err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// ====================================================================
// 核心接口 2: 读取邮件
//   GET /api/inbox?account_id=acc_xxx[&alias=xxx@icloud.com][&limit=20][&days=7]
//
//   - 不传 alias: 返回该账号收件箱最近邮件
//   - 传 alias:   只返回发给该 HME 别名的邮件
//
//   认证优先级: IMAP (App Password) 优先 > Web API (Cookie) 回退
//   - IMAP: 支持服务端按收件人搜索 (FindByRecipient)
//   - Web API: 不支持收件人搜索,拉取收件箱后本地按别名过滤 (FindByAlias)
// ====================================================================

func (s *Server) listInbox(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	alias := strings.TrimSpace(c.Query("alias"))
	folder := strings.TrimSpace(c.DefaultQuery("folder", "inbox"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	withBody := c.Query("body") == "1" || c.Query("body") == "true"

	// 优先使用 IMAP (App Password 认证)
	mc, err := s.mgr.MailClient(accountID)
	if err == nil {
		if connErr := mc.Connect(); connErr == nil {
			defer mc.Disconnect()
			var messages []mail.Message
			if alias != "" {
				if withBody {
					messages, err = mc.FindByRecipientInFolderWithBodies(alias, folder, limit, days)
				} else {
					messages, err = mc.FindByRecipientInFolder(alias, folder, limit, days)
				}
			} else if withBody {
				messages, err = mc.ListFolderWithBodies(folder, limit, days)
			} else {
				messages, err = mc.ListFolder(folder, limit, days)
			}
			if err == nil {
				ok(c, gin.H{
					"account_id": accountID,
					"alias":      alias,
					"folder":     folder,
					"count":      len(messages),
					"messages":   messages,
					"method":     "imap",
				})
				return
			}
			// IMAP 失败，继续尝试 Web API
		}
	}

	// 回退到 Web API (Cookie 认证，无需 App Password)
	wmc, err := s.mgr.WebMailClient(accountID)
	if err != nil {
		fail(c, http.StatusBadRequest, "无可用邮件客户端: 需要 App Password 或 Cookie")
		return
	}
	if folder != "" && folder != "inbox" && folder != "all" {
		fail(c, http.StatusBadRequest, "Web API 回退暂不支持读取该文件夹: "+folder)
		return
	}

	if alias != "" {
		messages, err := wmc.FindByAlias(alias, limit)
		if err != nil {
			fail(c, http.StatusBadGateway, "读取邮件失败: "+err.Error())
			return
		}
		ok(c, gin.H{
			"account_id": accountID,
			"alias":      alias,
			"folder":     folder,
			"count":      len(messages),
			"messages":   messages,
			"method":     "web_api",
		})
	} else {
		messages, err := wmc.ListInbox(limit)
		if err != nil {
			fail(c, http.StatusBadGateway, "读取邮件失败: "+err.Error())
			return
		}
		ok(c, gin.H{
			"account_id": accountID,
			"folder":     folder,
			"count":      len(messages),
			"messages":   messages,
			"method":     "web_api",
		})
	}
}

func (s *Server) getMessage(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	folder := strings.TrimSpace(c.DefaultQuery("folder", "INBOX"))
	uidText := strings.TrimSpace(c.Param("id"))
	if strings.Contains(uidText, ":") && folder == "INBOX" {
		parts := strings.SplitN(uidText, ":", 2)
		folder = parts[0]
		uidText = parts[1]
	}
	uid64, err := strconv.ParseUint(uidText, 10, 32)
	if err != nil {
		fail(c, http.StatusBadRequest, "邮件 ID 必须是 IMAP UID")
		return
	}
	uid := uint32(uid64)

	if message, cached := s.cachedMessage(accountID, folder, uid); cached {
		ok(c, gin.H{
			"account_id": accountID,
			"message":    message,
			"method":     "cache",
			"cached":     true,
		})
		return
	}

	mc, err := s.mgr.MailClient(accountID)
	if err != nil {
		fail(c, http.StatusBadRequest, "读取邮件详情需要 App Password: "+err.Error())
		return
	}
	if err := mc.Connect(); err != nil {
		fail(c, http.StatusBadGateway, "IMAP 连接失败: "+err.Error())
		return
	}
	defer mc.Disconnect()

	message, err := mc.GetFullInFolder(folder, uid)
	if err != nil {
		fail(c, http.StatusBadGateway, "读取邮件详情失败: "+err.Error())
		return
	}
	s.setCachedMessage(accountID, folder, uid, message)
	ok(c, gin.H{
		"account_id": accountID,
		"message":    message,
		"method":     "imap",
	})
}

type messageRef struct {
	Folder string `json:"folder"`
	UID    string `json:"uid"`
}

type messagesReq struct {
	AccountID string       `json:"account_id" binding:"required"`
	Messages  []messageRef `json:"messages" binding:"required"`
}

func (s *Server) getMessages(c *gin.Context) {
	var req messagesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: account_id, messages 必填 — "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		ok(c, gin.H{
			"account_id": req.AccountID,
			"messages":   []*mail.FullMessage{},
			"method":     "cache",
			"cached":     true,
		})
		return
	}
	if len(req.Messages) > 50 {
		fail(c, http.StatusBadRequest, "一次最多预取 50 封邮件")
		return
	}

	byFolder := make(map[string][]uint32)
	seen := make(map[string]bool)
	out := make([]*mail.FullMessage, 0, len(req.Messages))
	for _, ref := range req.Messages {
		folder := strings.TrimSpace(ref.Folder)
		if folder == "" {
			folder = "INBOX"
		}
		uid64, err := strconv.ParseUint(strings.TrimSpace(ref.UID), 10, 32)
		if err != nil {
			fail(c, http.StatusBadRequest, "邮件 UID 必须是数字")
			return
		}
		uid := uint32(uid64)
		key := messageCacheKey(req.AccountID, folder, uid)
		if seen[key] {
			continue
		}
		seen[key] = true
		if message, cached := s.cachedMessage(req.AccountID, folder, uid); cached {
			out = append(out, message)
			continue
		}
		byFolder[folder] = append(byFolder[folder], uid)
	}

	method := "cache"
	if len(byFolder) > 0 {
		method = "imap"
		mc, err := s.mgr.MailClient(req.AccountID)
		if err != nil {
			fail(c, http.StatusBadRequest, "读取邮件详情需要 App Password: "+err.Error())
			return
		}
		if err := mc.Connect(); err != nil {
			fail(c, http.StatusBadGateway, "IMAP 连接失败: "+err.Error())
			return
		}
		defer mc.Disconnect()

		for folder, uids := range byFolder {
			messages, err := mc.GetFullBatchInFolder(folder, uids)
			if err != nil {
				fail(c, http.StatusBadGateway, "批量读取邮件详情失败: "+err.Error())
				return
			}
			for _, message := range messages {
				uid64, err := strconv.ParseUint(message.UID, 10, 32)
				if err == nil {
					s.setCachedMessage(req.AccountID, folder, uint32(uid64), message)
				}
				out = append(out, message)
			}
		}
	}
	ok(c, gin.H{
		"account_id": req.AccountID,
		"count":      len(out),
		"messages":   out,
		"method":     method,
		"cached":     len(byFolder) == 0,
	})
}

func (s *Server) listMailboxes(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	if c.Query("refresh") != "1" {
		if folders, cached := s.cachedMailboxes(accountID); cached {
			ok(c, gin.H{
				"account_id": accountID,
				"folders":    folders,
				"cached":     true,
			})
			return
		}
	}

	mc, err := s.mgr.MailClient(accountID)
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件夹需要 App Password: "+err.Error())
		return
	}
	if err := mc.Connect(); err != nil {
		fail(c, http.StatusBadGateway, "IMAP 连接失败: "+err.Error())
		return
	}
	defer mc.Disconnect()

	folders, err := mc.ListMailboxes()
	if err != nil {
		fail(c, http.StatusBadGateway, "读取文件夹失败: "+err.Error())
		return
	}
	s.setCachedMailboxes(accountID, folders)
	ok(c, gin.H{
		"account_id": accountID,
		"folders":    folders,
	})
}

// ====================================================================
// 辅助接口
// ====================================================================

func (s *Server) listAccounts(c *gin.Context) {
	ok(c, s.mgr.ListAccounts())
}

type addAccountReq struct {
	Name    string `json:"name" binding:"required"`
	Cookies string `json:"cookies"` // 可选,后续可通过 /login 获取
	Host    string `json:"host"`
	Proxy   string `json:"proxy"` // HTTP/SOCKS5 代理
}

func (s *Server) addAccount(c *gin.Context) {
	var req addAccountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: name 必填 — "+err.Error())
		return
	}
	acc, err := s.mgr.AddAccount(req.Name, req.Cookies, req.Host, req.Proxy)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 返回时脱敏
	acc.Cookies = nil
	c.JSON(http.StatusCreated, apiResp{Success: true, Data: acc})
}

func (s *Server) removeAccount(c *gin.Context) {
	id := c.Param("id")
	if !s.mgr.RemoveAccount(id) {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	ok(c, gin.H{"id": id})
}

type setPwdReq struct {
	ICloudEmail string `json:"icloud_email" binding:"required"`
	AppPassword string `json:"app_password" binding:"required"`
}

func (s *Server) setAppPassword(c *gin.Context) {
	id := c.Param("id")
	var req setPwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: icloud_email, app_password 必填 — "+err.Error())
		return
	}
	if err := s.mgr.SetAppPassword(id, req.ICloudEmail, req.AppPassword); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.clearCache()
	ok(c, gin.H{"id": id, "icloud_email": req.ICloudEmail})
}

type updateCookiesReq struct {
	Cookies map[string]string `json:"cookies" binding:"required"`
}

func (s *Server) updateCookies(c *gin.Context) {
	id := c.Param("id")
	var req updateCookiesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: cookies 必填 — "+err.Error())
		return
	}
	if err := s.mgr.UpdateCookies(id, req.Cookies); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.clearCache()
	ok(c, gin.H{"id": id, "cookies_count": len(req.Cookies)})
}

type loginReq struct {
	Password string `json:"password" binding:"required"`
	OTPCode  string `json:"otp_code"` // 可选 2FA 验证码
}

func (s *Server) loginAccount(c *gin.Context) {
	id := c.Param("id")
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: password 必填 — "+err.Error())
		return
	}

	var otpProvider hme.OTPProvider
	if req.OTPCode != "" {
		otp := req.OTPCode
		otpProvider = func() (string, error) {
			return otp, nil
		}
	}

	client, err := s.mgr.HMEClientWithPassword(id, req.Password, otpProvider)
	if err != nil {
		if isSessionError(err.Error()) {
			fail(c, http.StatusUnauthorized, err.Error())
		} else {
			fail(c, http.StatusBadGateway, "登录失败: "+err.Error())
		}
		return
	}

	s.clearCache()
	ok(c, gin.H{
		"id":      id,
		"cookies": client.Cookies,
	})
}

func (s *Server) listAliases(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	if c.Query("refresh") != "1" {
		if aliases, cached := s.cachedAliases(accountID); cached {
			ok(c, gin.H{
				"account_id": accountID,
				"count":      len(aliases),
				"aliases":    aliases,
				"cached":     true,
			})
			return
		}
	}
	client, err := s.mgr.HMEClient(accountID, false)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	aliases, err := client.ListAliases()
	_ = s.mgr.SaveCookies(accountID, client.Cookies)
	if err != nil {
		if isSessionError(err.Error()) {
			fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		} else {
			fail(c, http.StatusBadGateway, err.Error())
		}
		return
	}
	s.setCachedAliases(accountID, aliases)
	ok(c, gin.H{
		"account_id": accountID,
		"count":      len(aliases),
		"aliases":    aliases,
	})
}

type aliasActionReq struct {
	AccountID string `json:"account_id" binding:"required"`
}

func (s *Server) deactivateAlias(c *gin.Context) {
	anonymousID := c.Param("id")
	var req aliasActionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: account_id 必填 — "+err.Error())
		return
	}

	client, err := s.mgr.HMEClient(req.AccountID, false)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}

	success, err := client.DeactivateHME(anonymousID)
	_ = s.mgr.SaveCookies(req.AccountID, client.Cookies)
	if err != nil {
		fail(c, http.StatusBadGateway, "停用失败: "+err.Error())
		return
	}
	s.invalidateAliases(req.AccountID)
	ok(c, gin.H{"anonymous_id": anonymousID, "success": success})
}

func (s *Server) reactivateAlias(c *gin.Context) {
	anonymousID := c.Param("id")
	var req aliasActionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: account_id 必填 — "+err.Error())
		return
	}

	client, err := s.mgr.HMEClient(req.AccountID, false)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}

	success, err := client.ReactivateHME(anonymousID)
	_ = s.mgr.SaveCookies(req.AccountID, client.Cookies)
	if err != nil {
		fail(c, http.StatusBadGateway, "激活失败: "+err.Error())
		return
	}
	s.invalidateAliases(req.AccountID)
	ok(c, gin.H{"anonymous_id": anonymousID, "success": success})
}

func (s *Server) deleteAlias(c *gin.Context) {
	anonymousID := c.Param("id")
	var req aliasActionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: account_id 必填 — "+err.Error())
		return
	}

	client, err := s.mgr.HMEClient(req.AccountID, false)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}

	if err := client.Delete(anonymousID); err != nil {
		_ = s.mgr.SaveCookies(req.AccountID, client.Cookies)
		fail(c, http.StatusBadGateway, "删除失败: "+err.Error())
		return
	}
	_ = s.mgr.SaveCookies(req.AccountID, client.Cookies)
	s.invalidateAliases(req.AccountID)
	ok(c, gin.H{"anonymous_id": anonymousID})
}

// isSessionError 判断错误是否由会话失效引起。
func isSessionError(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "401") || strings.Contains(m, "403") ||
		strings.Contains(m, "session") || strings.Contains(m, "cookie") ||
		strings.Contains(m, "unauthorized") || strings.Contains(m, "认证") ||
		strings.Contains(m, "会话校验失败")
}

// reloadConfig 重新加载 accounts.json 配置文件。
func (s *Server) reloadConfig(c *gin.Context) {
	if err := s.mgr.Reload(); err != nil {
		fail(c, http.StatusInternalServerError, "重新加载配置失败: "+err.Error())
		return
	}
	s.clearCache()
	ok(c, gin.H{"message": "配置已重新加载"})
}

// 确保 hme 包被引用(类型在 handler 中使用)
var _ = hme.Alias{}
