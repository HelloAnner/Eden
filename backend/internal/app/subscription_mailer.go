// Package app 提供订阅邮件异步发送能力，基于扫描新增笔记触发通知。
// Author: Codex
// Created: 2026-02-16
package app

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"net/smtp"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const subscriberSendInterval = 3 * time.Second

type MailSender interface {
	Send(cfg MailConfig, to string, subject string, htmlBody string) error
}

type SubscriptionMailer struct {
	mu      sync.RWMutex
	store   *Store
	config  BlogConfig
	baseURL string
	queue   chan mailTask
	sender  MailSender
}

type mailTask struct {
	Article   Article
	Recipient Subscriber
}

type smtpMailSender struct{}

func NewSubscriptionMailer(config BlogConfig, store *Store) *SubscriptionMailer {
	mailer := &SubscriptionMailer{
		store:   store,
		config:  config,
		baseURL: normalizeBaseURL(config.Site.BaseURL),
		queue:   make(chan mailTask, 4096),
		sender:  smtpMailSender{},
	}
	go mailer.runWorker()
	LogInfof("mailer", "subscription mailer initialized")
	return mailer
}

func (m *SubscriptionMailer) UpdateConfig(config BlogConfig) {
	m.mu.Lock()
	m.config = config
	m.baseURL = normalizeBaseURL(config.Site.BaseURL)
	m.mu.Unlock()
	LogInfof("mailer", "mailer config updated, enabled=%t, smtp_host=%s", config.Mail.Enabled, config.Mail.SMTPHost)
}

func (m *SubscriptionMailer) EnqueueNewArticles(articles []Article) {
	if len(articles) == 0 {
		return
	}
	cfg, _ := m.snapshotConfig()
	if !cfg.Mail.Enabled {
		LogDebugf("mailer", "skip enqueue because mail is disabled")
		return
	}

	subscribers, err := m.store.ListActiveSubscribers()
	if err != nil {
		LogErrorf("mailer", "list subscribers failed: %v", err)
		return
	}
	if len(subscribers) == 0 {
		LogInfof("mailer", "skip enqueue because no active subscribers")
		return
	}

	for _, article := range articles {
		for _, subscriber := range subscribers {
			m.queue <- mailTask{
				Article:   article,
				Recipient: subscriber,
			}
		}
	}
	LogInfof("mailer", "queued subscription tasks, articles=%d, subscribers=%d, queue_size=%d", len(articles), len(subscribers), len(m.queue))
}

func (m *SubscriptionMailer) runWorker() {
	for task := range m.queue {
		err := m.sendTask(task)
		if err != nil {
			LogErrorf("mailer", "send subscription mail failed, article=%s, to=%s, err=%v", task.Article.ID, task.Recipient.Email, err)
			_ = m.store.UpdateSubscriberDelivery(task.Recipient.Email, "", err.Error())
		} else {
			LogInfof("mailer", "send subscription mail success, article=%s, to=%s", task.Article.ID, task.Recipient.Email)
			_ = m.store.UpdateSubscriberDelivery(task.Recipient.Email, time.Now().Format(time.RFC3339), "")
		}
		time.Sleep(subscriberSendInterval)
	}
}

func (m *SubscriptionMailer) sendTask(task mailTask) error {
	cfg, baseURL := m.snapshotConfig()
	subjectPrefix := strings.TrimSpace(cfg.Mail.SubjectPrefix)
	subject := task.Article.Title
	if subjectPrefix != "" {
		subject = subjectPrefix + task.Article.Title
	}
	htmlBody, err := renderArticleEmailHTML(task.Article, baseURL)
	if err != nil {
		return err
	}
	return m.sender.Send(cfg.Mail, task.Recipient.Email, subject, htmlBody)
}

func (m *SubscriptionMailer) snapshotConfig() (BlogConfig, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config, m.baseURL
}

func (smtpMailSender) Send(cfg MailConfig, to string, subject string, htmlBody string) error {
	if !cfg.Enabled {
		return nil
	}
	host := strings.TrimSpace(cfg.SMTPHost)
	from := strings.TrimSpace(cfg.From)
	if host == "" || cfg.SMTPPort <= 0 || from == "" {
		return fmt.Errorf("invalid smtp config")
	}

	client, closer, err := dialSMTPClient(cfg)
	if err != nil {
		return err
	}
	defer closer()

	username := strings.TrimSpace(cfg.Username)
	if username != "" {
		auth := smtp.PlainAuth("", username, cfg.Password, host)
		if err = client.Auth(auth); err != nil {
			return err
		}
	}

	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	defer writer.Close()

	message := buildHTMLMailMessage(from, to, subject, htmlBody)
	if _, err = writer.Write([]byte(message)); err != nil {
		return err
	}
	return nil
}

func dialSMTPClient(cfg MailConfig) (*smtp.Client, func(), error) {
	host := strings.TrimSpace(cfg.SMTPHost)
	addr := fmt.Sprintf("%s:%d", host, cfg.SMTPPort)
	encryption := strings.ToLower(strings.TrimSpace(cfg.Encryption))
	if encryption == "" {
		encryption = "starttls"
	}

	switch encryption {
	case "ssl", "tls":
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return nil, func() {}, err
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			_ = conn.Close()
			return nil, func() {}, err
		}
		return client, func() {
			_ = client.Quit()
		}, nil
	case "none":
		client, err := smtp.Dial(addr)
		if err != nil {
			return nil, func() {}, err
		}
		return client, func() {
			_ = client.Quit()
		}, nil
	default:
		client, err := smtp.Dial(addr)
		if err != nil {
			return nil, func() {}, err
		}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err = client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				_ = client.Close()
				return nil, func() {}, err
			}
		}
		return client, func() {
			_ = client.Quit()
		}, nil
	}
}

func buildHTMLMailMessage(from string, to string, subject string, htmlBody string) string {
	encodedSubject := mime.BEncoding.Encode("UTF-8", subject)
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: =?UTF-8?B?" + encodedSubject + "?=",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
	}
	return strings.Join(headers, "\r\n") + htmlBody
}

func renderArticleEmailHTML(article Article, baseURL string) (string, error) {
	content := preprocessObsidianForEmail(article.Content, article.Title, article.ID, baseURL)
	var bodyHTML bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(content), &bodyHTML); err != nil {
		return "", err
	}
	articleURL := buildAbsoluteURL(baseURL, "/article/"+url.PathEscape(article.ID))
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>%s</title>
  <style>
    body { margin: 0; padding: 24px; background: #F8FAFC; color: #1F2937; font-family: -apple-system,BlinkMacSystemFont,Segoe UI,Roboto,PingFang SC,Hiragino Sans GB,Microsoft YaHei,sans-serif; line-height: 1.75; }
    .container { max-width: 920px; margin: 0 auto; background: #FFFFFF; border: 1px solid #E5E7EB; border-radius: 12px; padding: 28px; }
    .meta { color: #6B7280; font-size: 13px; margin-bottom: 18px; }
    .article-title { margin: 0 0 6px 0; font-size: 30px; color: #111827; }
    .content img { max-width: 100%%; border-radius: 8px; }
    .content pre { background: #F3F4F6; padding: 12px; border-radius: 8px; overflow: auto; }
    .content code { background: #F3F4F6; padding: 2px 6px; border-radius: 4px; }
    .content a { color: #3141F5; text-decoration: none; }
    .content table { border-collapse: collapse; width: 100%%; margin: 12px 0; }
    .content th, .content td { border: 1px solid #E5E7EB; padding: 8px 10px; text-align: left; }
    .footer { margin-top: 30px; font-size: 13px; color: #6B7280; }
  </style>
</head>
<body>
  <div class="container">
    <h1 class="article-title">%s</h1>
    <div class="meta">路径：%s</div>
    <div class="content">%s</div>
    <div class="footer">原文链接：<a href="%s">%s</a></div>
  </div>
</body>
</html>`, escapeHTML(article.Title), escapeHTML(article.Title), escapeHTML(article.Path), bodyHTML.String(), articleURL, articleURL), nil
}

var (
	embedLinkPattern = regexp.MustCompile(`!\[\[([^[\]]+)\]\]`)
	wikiLinkPattern  = regexp.MustCompile(`\[\[([^[\]]+)\]\]`)
	highlightPattern = regexp.MustCompile(`==([^=\n]+)==`)
)

func preprocessObsidianForEmail(content string, articleTitle string, articleID string, baseURL string) string {
	normalized := normalizeEmailMarkdown(content, articleTitle)
	lines := strings.Split(normalized, "\n")
	converted := make([]string, 0, len(lines))
	inCodeFence := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeFence = !inCodeFence
			converted = append(converted, line)
			continue
		}
		if inCodeFence {
			converted = append(converted, line)
			continue
		}

		nextLine := transformCalloutHeaderForEmail(line)
		nextLine = embedLinkPattern.ReplaceAllStringFunc(nextLine, func(raw string) string {
			matches := embedLinkPattern.FindStringSubmatch(raw)
			if len(matches) < 2 {
				return raw
			}
			return convertEmbedForEmail(matches[1], articleID, baseURL)
		})
		nextLine = wikiLinkPattern.ReplaceAllStringFunc(nextLine, func(raw string) string {
			if strings.HasPrefix(raw, "![") {
				return raw
			}
			matches := wikiLinkPattern.FindStringSubmatch(raw)
			if len(matches) < 2 {
				return raw
			}
			return convertWikiLinkForEmail(matches[1], articleID, baseURL)
		})
		nextLine = highlightPattern.ReplaceAllString(nextLine, `<mark>$1</mark>`)
		converted = append(converted, nextLine)
	}
	return strings.TrimSpace(strings.Join(converted, "\n"))
}

func normalizeEmailMarkdown(content string, articleTitle string) string {
	source := strings.ReplaceAll(content, "\r\n", "\n")
	source = removeFrontmatterForEmail(source)
	lines := strings.Split(source, "\n")

	firstNonEmpty := -1
	for index, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstNonEmpty = index
			break
		}
	}
	if firstNonEmpty >= 0 && strings.TrimSpace(lines[firstNonEmpty]) == "# "+articleTitle {
		lines[firstNonEmpty] = ""
	}
	lines = removeTrailingTagBlockForEmail(lines)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func removeFrontmatterForEmail(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return content
	}
	return content[end+9:]
}

func removeTrailingTagBlockForEmail(lines []string) []string {
	index := len(lines) - 1
	for index >= 0 && strings.TrimSpace(lines[index]) == "" {
		index--
	}
	if index < 0 {
		return lines
	}
	hasTag := false
	start := index
	for start >= 0 {
		line := strings.TrimSpace(lines[start])
		if line == "" {
			start--
			continue
		}
		if isTagLineForEmail(line) {
			hasTag = true
			start--
			continue
		}
		break
	}
	if !hasTag {
		return lines
	}
	return lines[:start+1]
}

func isTagLineForEmail(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	if strings.HasPrefix(line, "# ") {
		return false
	}
	if strings.HasPrefix(line, "##") {
		return false
	}
	return true
}

func transformCalloutHeaderForEmail(line string) string {
	pattern := regexp.MustCompile(`^(\s*>\s*)\[!([A-Za-z0-9_-]+)([+-])?\]\s*(.*)$`)
	matches := pattern.FindStringSubmatch(line)
	if len(matches) != 5 {
		return line
	}
	prefix := matches[1]
	calloutType := strings.ToLower(strings.TrimSpace(matches[2]))
	title := strings.TrimSpace(matches[4])
	label := mapCalloutLabel(calloutType)
	if title == "" {
		return prefix + "**" + label + "**"
	}
	return prefix + "**" + label + ": " + title + "**"
}

func mapCalloutLabel(calloutType string) string {
	calloutMap := map[string]string{
		"note":      "📝 Note",
		"abstract":  "📌 Abstract",
		"summary":   "📌 Summary",
		"tldr":      "📌 TL;DR",
		"info":      "ℹ️ Info",
		"todo":      "✅ Todo",
		"tip":       "💡 Tip",
		"hint":      "💡 Hint",
		"important": "💡 Important",
		"success":   "✅ Success",
		"check":     "✅ Check",
		"done":      "✅ Done",
		"question":  "❓ Question",
		"help":      "❓ Help",
		"faq":       "❓ FAQ",
		"warning":   "⚠️ Warning",
		"caution":   "⚠️ Caution",
		"attention": "⚠️ Attention",
		"failure":   "❌ Failure",
		"fail":      "❌ Fail",
		"missing":   "❌ Missing",
		"danger":    "🚨 Danger",
		"error":     "🚨 Error",
		"bug":       "🐞 Bug",
		"example":   "🧪 Example",
		"quote":     "💬 Quote",
		"cite":      "💬 Cite",
	}
	if value, ok := calloutMap[calloutType]; ok {
		return value
	}
	return "📎 " + calloutType
}

func convertEmbedForEmail(rawValue string, articleID string, baseURL string) string {
	cleaned := strings.ReplaceAll(rawValue, `\|`, "|")
	parts := strings.Split(cleaned, "|")
	target := strings.TrimSpace(parts[0])
	if target == "" {
		return ""
	}
	lower := strings.ToLower(target)
	if strings.HasSuffix(lower, ".md") {
		noteName := strings.TrimSuffix(target, ".md")
		searchURL := buildAbsoluteURL(baseURL, "/search?q="+url.QueryEscape(noteName))
		return fmt.Sprintf("[🧩 嵌入笔记：%s](%s)", displayNameForEmail(noteName), searchURL)
	}
	fileURL := resolveAttachmentURLForEmail(target, articleID, baseURL)
	if isImageFileForEmail(lower) {
		return fmt.Sprintf("![%s](%s)", displayNameForEmail(target), fileURL)
	}
	return fmt.Sprintf("[📎 嵌入文件：%s](%s)", displayNameForEmail(target), fileURL)
}

func convertWikiLinkForEmail(rawValue string, articleID string, baseURL string) string {
	cleaned := strings.ReplaceAll(rawValue, `\|`, "|")
	parts := strings.Split(cleaned, "|")
	target := strings.TrimSpace(parts[0])
	alias := ""
	if len(parts) > 1 {
		alias = strings.TrimSpace(parts[1])
	}
	if target == "" {
		return alias
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		text := alias
		if text == "" {
			text = displayNameForEmail(target)
		}
		return fmt.Sprintf("[%s](%s)", text, target)
	}
	if strings.HasPrefix(target, "#") {
		heading := strings.TrimSpace(strings.TrimLeft(target, "#"))
		text := alias
		if text == "" {
			text = heading
		}
		return fmt.Sprintf("[%s](#%s)", text, slugifyHeadingForEmail(heading))
	}

	lower := strings.ToLower(target)
	if isImageFileForEmail(lower) {
		fileURL := resolveAttachmentURLForEmail(target, articleID, baseURL)
		text := alias
		if text == "" {
			text = displayNameForEmail(target)
		}
		return fmt.Sprintf("[%s](%s)", text, fileURL)
	}

	noteName := strings.TrimSuffix(target, ".md")
	text := alias
	if text == "" {
		text = displayNameForEmail(noteName)
	}
	searchURL := buildAbsoluteURL(baseURL, "/search?q="+url.QueryEscape(noteName))
	return fmt.Sprintf("[%s](%s)", text, searchURL)
}

func resolveAttachmentURLForEmail(target string, articleID string, baseURL string) string {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "data:") {
		return target
	}
	cleanTarget := strings.TrimPrefix(strings.TrimSpace(target), "./")
	if strings.HasPrefix(cleanTarget, "/") {
		return buildAbsoluteURL(baseURL, "/api/v1/data/"+encodePathSegmentsForEmail(strings.TrimPrefix(cleanTarget, "/")))
	}
	if strings.HasPrefix(cleanTarget, "attachments/") {
		return buildAbsoluteURL(baseURL, "/api/v1/data/"+encodePathSegmentsForEmail(articleID+"/"+cleanTarget))
	}
	if strings.Contains(cleanTarget, "/") {
		return buildAbsoluteURL(baseURL, "/api/v1/data/"+encodePathSegmentsForEmail(cleanTarget))
	}
	return buildAbsoluteURL(baseURL, "/api/v1/data/"+encodePathSegmentsForEmail(articleID+"/attachments/"+cleanTarget))
}

func encodePathSegmentsForEmail(path string) string {
	segments := strings.Split(path, "/")
	encoded := make([]string, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		encoded = append(encoded, url.PathEscape(segment))
	}
	return strings.Join(encoded, "/")
}

func buildAbsoluteURL(baseURL string, relative string) string {
	trimmedBase := normalizeBaseURL(baseURL)
	if trimmedBase == "" {
		return relative
	}
	if strings.HasPrefix(relative, "http://") || strings.HasPrefix(relative, "https://") {
		return relative
	}
	if !strings.HasPrefix(relative, "/") {
		relative = "/" + relative
	}
	return trimmedBase + relative
}

func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	trimmed = strings.TrimRight(trimmed, "/")
	return trimmed
}

func slugifyHeadingForEmail(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return ""
	}
	replacer := regexp.MustCompile(`[^\w\u4e00-\u9fa5]+`)
	normalized := replacer.ReplaceAllString(lower, "-")
	return strings.Trim(normalized, "-")
}

func isImageFileForEmail(value string) bool {
	return strings.HasSuffix(value, ".png") ||
		strings.HasSuffix(value, ".jpg") ||
		strings.HasSuffix(value, ".jpeg") ||
		strings.HasSuffix(value, ".gif") ||
		strings.HasSuffix(value, ".svg") ||
		strings.HasSuffix(value, ".webp") ||
		strings.HasSuffix(value, ".bmp") ||
		strings.HasSuffix(value, ".avif")
}

func displayNameForEmail(value string) string {
	normalized := strings.TrimSpace(strings.TrimSuffix(value, ".md"))
	if normalized == "" {
		return value
	}
	parts := strings.Split(normalized, "/")
	return parts[len(parts)-1]
}

func escapeHTML(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(value)
}
