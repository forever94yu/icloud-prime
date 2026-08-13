// Package mail 实现 iCloud 邮件 IMAP 读取客户端。
//
// 通过 Apple 应用专用密码连接 imap.mail.me.com:993,
// 拉取隐私邮箱别名收到的邮件。对应原 Python 项目 icloud_mail.py。
package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
)

const (
	IMAPServer = "imap.mail.me.com"
	IMAPPort   = 993
)

// Message 是一封邮件的摘要信息。
type Message struct {
	ID      string `json:"id"`
	UID     string `json:"uid,omitempty"`
	Folder  string `json:"folder,omitempty"`
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Preview string `json:"preview"`
	match   string
}

// Folder describes a selectable IMAP mailbox.
type Folder struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// FullMessage 是一封邮件的完整内容(含正文)。
type FullMessage struct {
	Message
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
}

// Client 是 iCloud 邮件 IMAP 客户端。
type Client struct {
	appleID     string
	appPassword string
	cli         *client.Client
}

// NewClient 创建 IMAP 客户端。需在调用其它方法前先 Connect。
func NewClient(appleID, appPassword string) *Client {
	return &Client{appleID: appleID, appPassword: appPassword}
}

// Connect 连接并登录 IMAP 服务器。
func (c *Client) Connect() error {
	addr := fmt.Sprintf("%s:%d", IMAPServer, IMAPPort)
	cli, err := client.DialTLS(addr, nil)
	if err != nil {
		return fmt.Errorf("IMAP 连接失败: %w", err)
	}
	if err := cli.Login(c.appleID, c.appPassword); err != nil {
		return fmt.Errorf("IMAP 登录失败 — 请检查: 1) 应用专用密码是否正确 2) Apple ID: %s — %w", c.appleID, err)
	}
	c.cli = cli
	return nil
}

// Disconnect 登出并关闭连接。
func (c *Client) Disconnect() {
	if c.cli != nil {
		_ = c.cli.Logout()
		c.cli = nil
	}
}

// InboxCount 返回收件箱邮件总数。
func (c *Client) InboxCount() (int, error) {
	if c.cli == nil {
		return 0, fmt.Errorf("未连接")
	}
	mbox, err := c.cli.Select("INBOX", false)
	if err != nil {
		return 0, err
	}
	return int(mbox.Messages), nil
}

// ListMailboxes returns selectable folders with normalized roles.
func (c *Client) ListMailboxes() ([]Folder, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}

	ch := make(chan *imap.MailboxInfo, 32)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.List("", "*", ch)
	}()

	var folders []Folder
	for info := range ch {
		if hasAttr(info.Attributes, imap.NoSelectAttr) {
			continue
		}
		folders = append(folders, Folder{
			Name: info.Name,
			Role: folderRole(info.Name, info.Attributes),
		})
	}
	if err := <-done; err != nil {
		return nil, err
	}

	sort.SliceStable(folders, func(i, j int) bool {
		return folderSortRank(folders[i]) < folderSortRank(folders[j])
	})
	return folders, nil
}

// ListInbox 拉取收件箱最近 limit 封邮件摘要。
//
// days 用于过滤只看近 N 天的邮件(0 表示不限制)。
// 返回按时间倒序排列。
func (c *Client) ListInbox(limit int, days int) ([]Message, error) {
	return c.ListFolder("inbox", limit, days)
}

// ListInboxWithBodies 拉取收件箱最近邮件,并解析正文用于验证码识别。
func (c *Client) ListInboxWithBodies(limit int, days int) ([]Message, error) {
	return c.ListFolderWithBodies("inbox", limit, days)
}

// ListFolder 拉取指定文件夹的最近邮件摘要。folder 可用 inbox、junk、all 或真实 IMAP 文件夹名。
func (c *Client) ListFolder(folder string, limit int, days int) ([]Message, error) {
	return c.listFolder(folder, limit, days, false)
}

// ListFolderWithBodies 拉取指定文件夹的最近邮件,并解析正文用于验证码识别。
func (c *Client) ListFolderWithBodies(folder string, limit int, days int) ([]Message, error) {
	return c.listFolder(folder, limit, days, true)
}

func (c *Client) listFolder(folder string, limit int, days int, includeBody bool) ([]Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 50
	}

	folders, err := c.resolveFolders(folder)
	if err != nil {
		return nil, err
	}

	var all []Message
	for _, name := range folders {
		messages, err := c.listMailbox(name, limit, days, includeBody)
		if err != nil {
			continue
		}
		all = append(all, messages...)
	}
	sortMessages(all)
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (c *Client) listMailbox(folder string, limit int, days int, includeBody bool) ([]Message, error) {
	mbox, err := c.cli.Select(folder, true)
	if err != nil {
		return nil, err
	}
	total := int(mbox.Messages)
	if total == 0 {
		return []Message{}, nil
	}

	// 计算起始序号(只取最近 limit 封)
	from := uint32(1)
	if uint32(limit) < mbox.Messages {
		from = mbox.Messages - uint32(limit) + 1
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	items := []imap.FetchItem{
		imap.FetchUid,
		imap.FetchEnvelope,
		imap.FetchInternalDate,
	}
	parser := toMessageSummary
	if includeBody {
		section := &imap.BodySectionName{Peek: true}
		items = append(items, section.FetchItem())
		parser = toMessageWithBody
	}

	messages := make(chan *imap.Message, limit)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.Fetch(seqset, items, messages)
	}()

	var out []Message
	for msg := range messages {
		m := parser(msg, folder)
		// days 过滤
		if days > 0 && olderThanDays(m.Date, days) {
			continue
		}
		out = append(out, m)
	}
	if err := <-done; err != nil {
		return nil, err
	}
	sortMessages(out)
	return out, nil
}

// FindByRecipient 查找发给指定隐私邮箱别名的邮件。
//
// 先尝试 IMAP TO 搜索;失败则拉取收件箱后本地过滤。
func (c *Client) FindByRecipient(recipient string, limit int, days int) ([]Message, error) {
	return c.FindByRecipientInFolder(recipient, "inbox", limit, days)
}

// FindByRecipientInFolder 查找指定文件夹中发给隐私邮箱别名的邮件。
func (c *Client) FindByRecipientInFolder(recipient string, folder string, limit int, days int) ([]Message, error) {
	return c.findByRecipientInFolder(recipient, folder, limit, days, false)
}

// FindByRecipientInFolderWithBodies 查找指定文件夹中发给隐私邮箱别名的邮件,并解析正文。
func (c *Client) FindByRecipientInFolderWithBodies(recipient string, folder string, limit int, days int) ([]Message, error) {
	return c.findByRecipientInFolder(recipient, folder, limit, days, true)
}

func (c *Client) findByRecipientInFolder(recipient string, folder string, limit int, days int, includeBody bool) ([]Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 20
	}

	folders, err := c.resolveFolders(folder)
	if err != nil {
		return nil, err
	}

	var out []Message
	seen := map[string]bool{}
	for _, name := range folders {
		messages, err := c.findByRecipientInMailbox(recipient, name, limit, days, includeBody)
		if err != nil {
			continue
		}
		for _, m := range messages {
			key := m.Folder + ":" + m.UID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, m)
		}
	}
	sortMessages(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c *Client) findByRecipientInMailbox(recipient string, folder string, limit int, days int, includeBody bool) ([]Message, error) {
	// 先尝试服务端 TO 搜索；部分 Hide My Email 邮件会把收件人改成“隐藏邮件地址”，所以后面还有本地兜底。
	if _, err := c.cli.Select(folder, true); err != nil {
		return nil, err
	}
	uids, err := c.searchRecipientUIDs(recipient, days)
	if err == nil && len(uids) > 0 {
		return c.fetchByUIDs(uids, folder, limit, includeBody)
	}

	all, err := c.listMailbox(folder, limit*3, days, true)
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, m := range all {
		if m.matches(recipient) {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (c *Client) searchRecipientUIDs(recipient string, days int) ([]uint32, error) {
	headers := []string{"To", "Delivered-To", "X-Original-To", "Envelope-To"}
	var lastErr error
	for _, header := range headers {
		criteria := imap.NewSearchCriteria()
		criteria.Header.Add(header, recipient)
		if days > 0 {
			criteria.Since = time.Now().AddDate(0, 0, -days)
		}
		uids, err := c.cli.UidSearch(criteria)
		if err == nil && len(uids) > 0 {
			return uids, nil
		}
		if err != nil && lastErr == nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (c *Client) fetchByUIDs(uids []uint32, folder string, limit int, includeBody bool) ([]Message, error) {
	if len(uids) == 0 {
		return []Message{}, nil
	}
	// 取最近 limit 条(UID 倒序)
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uid)
	}

	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate}
	parser := toMessageSummary
	if includeBody {
		section := &imap.BodySectionName{Peek: true}
		items = append(items, section.FetchItem())
		parser = toMessageWithBody
	}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	var out []Message
	for msg := range messages {
		out = append(out, parser(msg, folder))
	}
	if err := <-done; err != nil {
		return nil, err
	}
	sortMessages(out)
	return out, nil
}

// GetFull 获取单封邮件的完整内容(含正文)。
func (c *Client) GetFull(uid uint32) (*FullMessage, error) {
	return c.GetFullInFolder("INBOX", uid)
}

// GetFullInFolder 获取指定文件夹中单封邮件的完整内容。
func (c *Client) GetFullInFolder(folder string, uid uint32) (*FullMessage, error) {
	messages, err := c.GetFullBatchInFolder(folder, []uint32{uid})
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("邮件不存在 (uid=%d)", uid)
	}
	return messages[0], nil
}

// GetFullBatchInFolder 批量获取指定文件夹中的完整邮件内容。
func (c *Client) GetFullBatchInFolder(folder string, uids []uint32) ([]*FullMessage, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if folder == "" {
		folder = "INBOX"
	}
	if len(uids) == 0 {
		return []*FullMessage{}, nil
	}
	if _, err := c.cli.Select(folder, true); err != nil {
		return nil, err
	}

	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uid)
	}

	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	var out []*FullMessage
	for msg := range messages {
		if msg == nil {
			continue
		}
		message := toMessage(msg, folder)
		full := &FullMessage{Message: message}
		if r := msg.GetBody(section); r != nil {
			if em, err := mail.ReadMessage(r); err == nil {
				if body, err := readBody(em); err == nil {
					full.Body = strings.TrimSpace(body)
					full.Preview = full.Body
				}
				full.ContentType = em.Header.Get("Content-Type")
			}
		}
		out = append(out, full)
	}
	if err := <-done; err != nil {
		return nil, err
	}
	sortFullMessages(out)
	return out, nil
}

// ---- 解析工具 ----

func toMessage(msg *imap.Message, folder string) Message {
	m := Message{}
	if msg.Uid > 0 {
		m.UID = fmt.Sprintf("%d", msg.Uid)
		if folder != "" {
			m.ID = fmt.Sprintf("%s:%d", folder, msg.Uid)
			m.Folder = folder
		} else {
			m.ID = fmt.Sprintf("%d", msg.Uid)
		}
	}
	if msg.Envelope != nil {
		if len(msg.Envelope.From) > 0 {
			m.From = msg.Envelope.From[0].Address()
		}
		if len(msg.Envelope.To) > 0 {
			addrs := make([]string, 0, len(msg.Envelope.To))
			for _, a := range msg.Envelope.To {
				addrs = append(addrs, a.Address())
			}
			m.To = strings.Join(addrs, ", ")
		}
		m.Subject = decodeHeader(msg.Envelope.Subject)
		if !msg.Envelope.Date.IsZero() {
			m.Date = msg.Envelope.Date.Format(time.RFC3339)
		}
	}
	if m.From != "" {
		m.From = decodeHeader(m.From)
	}
	if m.To != "" {
		m.To = decodeHeader(m.To)
	}
	return m
}

func toMessageSummary(msg *imap.Message, folder string) Message {
	return toMessage(msg, folder)
}

// toMessageWithBody 在 toMessage 基础上解析正文填充 Preview(供 OTP 提取)。
func toMessageWithBody(msg *imap.Message, folder string) Message {
	m := toMessage(msg, folder)
	if r := msg.GetBody(&imap.BodySectionName{}); r != nil {
		if em, err := mail.ReadMessage(r); err == nil {
			if body, err := readBody(em); err == nil {
				m.Preview = strings.TrimSpace(body)
			}
			m.match = strings.Join([]string{
				m.From,
				m.To,
				m.Subject,
				headersText(em.Header),
				m.Preview,
			}, "\n")
		}
	}
	return m
}

// decodeHeader 解码 RFC 2047 编码的邮件头(如 =?UTF-8?B?xxx?=)。
func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	dec := mime.WordDecoder{CharsetReader: charset.Reader}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

func (c *Client) resolveFolders(folder string) ([]string, error) {
	folder = strings.TrimSpace(folder)
	role := strings.ToLower(folder)
	if folder == "" || role == "inbox" {
		return []string{"INBOX"}, nil
	}
	if role == "all" {
		return []string{"INBOX", "Junk"}, nil
	}
	if role == "junk" || role == "spam" {
		return []string{"Junk"}, nil
	}

	mailboxes, err := c.ListMailboxes()
	if err != nil {
		return []string{folder}, nil
	}

	var names []string
	for _, mbox := range mailboxes {
		switch role {
		case "all":
			if mbox.Role == "inbox" || mbox.Role == "junk" {
				names = append(names, mbox.Name)
			}
		case "junk", "spam":
			if mbox.Role == "junk" {
				names = append(names, mbox.Name)
			}
		default:
			if strings.EqualFold(mbox.Name, folder) || strings.EqualFold(mbox.Role, role) {
				names = append(names, mbox.Name)
			}
		}
	}
	if len(names) == 0 {
		if role == "all" {
			return []string{"INBOX", "Junk"}, nil
		}
		return []string{folder}, nil
	}
	return names, nil
}

func hasAttr(attrs []string, attr string) bool {
	for _, item := range attrs {
		if strings.EqualFold(item, attr) {
			return true
		}
	}
	return false
}

func folderRole(name string, attrs []string) string {
	switch {
	case strings.EqualFold(name, "INBOX"):
		return "inbox"
	case hasAttr(attrs, imap.JunkAttr):
		return "junk"
	case hasAttr(attrs, imap.SentAttr):
		return "sent"
	case hasAttr(attrs, imap.DraftsAttr):
		return "drafts"
	case hasAttr(attrs, imap.TrashAttr):
		return "trash"
	case hasAttr(attrs, imap.ArchiveAttr):
		return "archive"
	}

	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "junk"), strings.Contains(lower, "spam"), strings.Contains(lower, "bulk"):
		return "junk"
	case strings.Contains(lower, "sent"):
		return "sent"
	case strings.Contains(lower, "draft"):
		return "drafts"
	case strings.Contains(lower, "trash"), strings.Contains(lower, "deleted"):
		return "trash"
	case strings.Contains(lower, "archive"):
		return "archive"
	default:
		return "custom"
	}
}

func folderSortRank(folder Folder) int {
	switch folder.Role {
	case "inbox":
		return 0
	case "junk":
		return 1
	case "archive":
		return 2
	case "sent":
		return 3
	case "drafts":
		return 4
	case "trash":
		return 5
	default:
		return 10
	}
}

func sortMessages(messages []Message) {
	sort.SliceStable(messages, func(i, j int) bool {
		ti, _ := parseMessageDate(messages[i].Date)
		tj, _ := parseMessageDate(messages[j].Date)
		if ti.IsZero() || tj.IsZero() {
			return messages[i].Date > messages[j].Date
		}
		return ti.After(tj)
	})
}

func sortFullMessages(messages []*FullMessage) {
	sort.SliceStable(messages, func(i, j int) bool {
		ti, _ := parseMessageDate(messages[i].Date)
		tj, _ := parseMessageDate(messages[j].Date)
		if ti.IsZero() || tj.IsZero() {
			return messages[i].Date > messages[j].Date
		}
		return ti.After(tj)
	})
}

func olderThanDays(value string, days int) bool {
	t, err := parseMessageDate(value)
	if err != nil {
		return false
	}
	return time.Since(t) > time.Duration(days)*24*time.Hour
}

func parseMessageDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析邮件时间: %s", value)
}

func headersText(header mail.Header) string {
	var builder strings.Builder
	for key, values := range header {
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(strings.Join(values, ", "))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func (m Message) matches(recipient string) bool {
	needle := strings.ToLower(strings.TrimSpace(recipient))
	if needle == "" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{
		m.From,
		m.To,
		m.Subject,
		m.Preview,
		m.match,
	}, "\n"))
	return strings.Contains(text, needle)
}

var (
	htmlCommentBlock  = regexp.MustCompile(`(?is)<!--.*?-->`)
	htmlHeadBlock     = regexp.MustCompile(`(?is)<head[^>]*>.*?</head>`)
	htmlStyleBlock    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	htmlScriptBlock   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlNoscriptBlock = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)
	htmlTag           = regexp.MustCompile(`<[^>]+>`)
	blankLines        = regexp.MustCompile(`\n{3,}`)
)

// readBody 读取邮件正文,优先 text/plain,其次从 HTML 提取纯文本。
func readBody(msg *mail.Message) (string, error) {
	return readMIMEBody(msg.Header, msg.Body)
}

func readMIMEBody(header mail.Header, body io.Reader) (string, error) {
	ct := header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", fmt.Errorf("multipart 邮件缺少 boundary")
		}
		return readMultipartBody(body, boundary)
	}

	raw, err := io.ReadAll(decodeTransfer(header.Get("Content-Transfer-Encoding"), body))
	if err != nil {
		return "", err
	}
	text := decodeBodyCharset(raw, ct)
	if mediaType == "text/html" {
		return stripHTML(text), nil
	}
	return text, nil
}

func readMultipartBody(body io.Reader, boundary string) (string, error) {
	mr := multipart.NewReader(body, boundary)
	var firstText string
	var htmlFallback string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		partType = strings.ToLower(partType)
		text, err := readMIMEBody(mail.Header(part.Header), part)
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		if strings.HasPrefix(partType, "text/plain") {
			return text, nil
		}
		if strings.HasPrefix(partType, "text/html") && htmlFallback == "" {
			htmlFallback = text
			continue
		}
		if firstText == "" {
			firstText = text
		}
	}

	if htmlFallback != "" {
		return htmlFallback, nil
	}
	return firstText, nil
}

func decodeTransfer(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	default:
		return body
	}
}

func decodeBodyCharset(raw []byte, contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return string(raw)
	}
	name := params["charset"]
	if name == "" || strings.EqualFold(name, "utf-8") || strings.EqualFold(name, "us-ascii") {
		return string(raw)
	}
	reader, err := charset.Reader(name, bytes.NewReader(raw))
	if err != nil {
		return string(raw)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return string(raw)
	}
	return string(decoded)
}

// stripHTML removes markup and non-content blocks, leaving readable text.
func stripHTML(markup string) string {
	markup = htmlCommentBlock.ReplaceAllString(markup, "")
	markup = htmlHeadBlock.ReplaceAllString(markup, "")
	markup = htmlStyleBlock.ReplaceAllString(markup, "")
	markup = htmlScriptBlock.ReplaceAllString(markup, "")
	markup = htmlNoscriptBlock.ReplaceAllString(markup, "")

	breakTags := []string{"<br>", "<br/>", "<br />", "</p>", "</div>", "</tr>", "</h1>", "</h2>", "</h3>", "</li>"}
	for _, tag := range breakTags {
		markup = strings.ReplaceAll(markup, tag, "\n")
		markup = strings.ReplaceAll(markup, strings.ToUpper(tag), "\n")
	}
	markup = strings.ReplaceAll(markup, "<li>", "\n- ")
	markup = strings.ReplaceAll(markup, "<LI>", "\n- ")

	markup = htmlTag.ReplaceAllString(markup, "")
	markup = stdhtml.UnescapeString(markup)
	markup = strings.ReplaceAll(markup, "\u00a0", " ")

	lines := strings.Split(markup, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
