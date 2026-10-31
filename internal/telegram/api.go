package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.telegram.org/bot"

type Client struct {
	Token  string
	HTTP   *http.Client
	Offset int64
}

func NewClient(token string) *Client {
	return &Client{
		Token: strings.TrimSpace(token),
		HTTP:  &http.Client{Timeout: 50 * time.Second},
	}
}

func (c *Client) enabled() bool { return c != nil && c.Token != "" }

type apiResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func (c *Client) call(method string, payload any) (json.RawMessage, error) {
	return c.callOnce(method, payload, false)
}

func (c *Client) callOnce(method string, payload any, retried bool) (json.RawMessage, error) {
	if !c.enabled() {
		return nil, fmt.Errorf("telegram token empty")
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+c.Token+"/"+method, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var ar apiResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("telegram decode: %w", err)
	}
	if !ar.OK {
		// 429: Telegram says how long to wait. One short, bounded retry keeps
		// broadcasts and bursts of replies from silently dropping messages.
		if resp.StatusCode == http.StatusTooManyRequests && !retried && method != "getUpdates" {
			wait := 1
			if m := retryAfterRE.FindStringSubmatch(ar.Description); len(m) == 2 {
				if n, e := strconv.Atoi(m[1]); e == nil && n > 0 {
					wait = n
				}
			}
			if wait <= 5 {
				time.Sleep(time.Duration(wait)*time.Second + 200*time.Millisecond)
				return c.callOnce(method, payload, true)
			}
		}
		return nil, fmt.Errorf("telegram: %s", ar.Description)
	}
	return ar.Result, nil
}

var retryAfterRE = regexp.MustCompile(`retry after (\d+)`)

type InlineKeyboardButton struct {
	Text         string      `json:"text"`
	CallbackData string      `json:"callback_data,omitempty"`
	URL          string      `json:"url,omitempty"`
	WebApp       *WebAppInfo `json:"web_app,omitempty"`
}

type WebAppInfo struct {
	URL string `json:"url"`
}

// ReplyKeyboardButton is a button on the persistent chat keyboard (next to the input field).
type ReplyKeyboardButton struct {
	Text   string      `json:"text"`
	WebApp *WebAppInfo `json:"web_app,omitempty"`
}

// ReplyKeyboardMarkup is shown under the message input (persistent main menu).
type ReplyKeyboardMarkup struct {
	Keyboard              [][]ReplyKeyboardButton `json:"keyboard"`
	IsPersistent          bool                    `json:"is_persistent"`
	ResizeKeyboard        bool                    `json:"resize_keyboard"`
	OneTimeKeyboard       bool                    `json:"one_time_keyboard,omitempty"`
	InputFieldPlaceholder string                  `json:"input_field_placeholder,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func (c *Client) SendMessage(chatID int64, text string) error {
	return c.SendMessageMarkup(chatID, text, nil)
}

func (c *Client) SendMessageMarkup(chatID int64, text string, markup *InlineKeyboardMarkup) error {
	return c.SendMessageFull(chatID, text, markup, nil)
}

// SendMessageReplyKB sends a message with a persistent reply keyboard under the input box.
func (c *Client) SendMessageReplyKB(chatID int64, text string, reply *ReplyKeyboardMarkup) error {
	return c.SendMessageFull(chatID, text, nil, reply)
}

func (c *Client) SendMessageFull(chatID int64, text string, inline *InlineKeyboardMarkup, reply *ReplyKeyboardMarkup) error {
	text = clipTelegramText(text)
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if inline != nil {
		payload["reply_markup"] = inline
	} else if reply != nil {
		payload["reply_markup"] = reply
	}
	_, err := c.call("sendMessage", payload)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "can't parse entities") {
		// User/admin supplied text with a stray "<" or "&" (ticket subject, plan
		// name, a clipped tag...) makes Telegram reject the whole message and the
		// customer receives nothing. Resend it as plain text instead.
		delete(payload, "parse_mode")
		payload["text"] = stripTelegramHTML(text)
		_, err = c.call("sendMessage", payload)
	}
	return err
}

var telegramTagRE = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)

// stripTelegramHTML removes HTML tags and decodes the three entities the bots emit.
func stripTelegramHTML(s string) string {
	s = telegramTagRE.ReplaceAllString(s, "")
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// clipTelegramText keeps a message within Telegram's 4096-character limit
// without cutting a multi-byte (Persian) character or leaving a half-written
// HTML tag/entity at the end. The old byte-based cut shortened Persian text to
// ~2000 characters and could corrupt the markup.
func clipTelegramText(text string) string {
	const limit = 4000
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	cut := string(r[:limit-10])
	if i := strings.LastIndex(cut, "\n"); i > len(cut)/2 {
		cut = cut[:i]
	}
	if lt, gt := strings.LastIndex(cut, "<"), strings.LastIndex(cut, ">"); lt > gt {
		cut = cut[:lt]
	}
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut + "…"
}

func (c *Client) AnswerCallback(id, text string) error {
	_, err := c.call("answerCallbackQuery", map[string]any{
		"callback_query_id": id,
		"text":              text,
		"show_alert":        false,
	})
	return err
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Message struct {
	MessageID int64     `json:"message_id"`
	Text      string    `json:"text"`
	Caption   string    `json:"caption"`
	Chat      Chat      `json:"chat"`
	From      *User     `json:"from"`
	Photo     []Photo   `json:"photo"`
	Document  *Document `json:"document"`
}

type Photo struct {
	FileID string `json:"file_id"`
}

type Document struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// DeleteWebhook removes any webhook so getUpdates long-polling works.
func (c *Client) DeleteWebhook() error {
	if !c.enabled() {
		return fmt.Errorf("telegram token empty")
	}
	_, err := c.call("deleteWebhook", map[string]any{"drop_pending_updates": false})
	return err
}

func (c *Client) GetUpdates(timeoutSec int) ([]Update, error) {
	result, err := c.call("getUpdates", map[string]any{
		"offset":          c.Offset,
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message", "callback_query"},
	})
	if err != nil {
		return nil, err
	}
	var updates []Update
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, err
	}
	for _, u := range updates {
		if u.UpdateID+1 > c.Offset {
			c.Offset = u.UpdateID + 1
		}
	}
	return updates, nil
}

// BotCommand is a Telegram bot menu command.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands registers the bot command menu (Telegram "/" menu).
func (c *Client) SetMyCommands(cmds []BotCommand) error {
	if !c.enabled() {
		return fmt.Errorf("telegram token empty")
	}
	_, err := c.call("setMyCommands", map[string]any{"commands": cmds})
	return err
}

func (c *Client) GetMe() (string, error) {
	result, err := c.call("getMe", map[string]any{})
	if err != nil {
		return "", err
	}
	var me struct {
		Username string `json:"username"`
	}
	_ = json.Unmarshal(result, &me)
	return me.Username, nil
}

// Broadcast sends text to many chat IDs (best-effort).
func (c *Client) Broadcast(chatIDs []int64, text string) (ok, fail int) {
	for _, id := range chatIDs {
		if err := c.SendMessage(id, text); err != nil {
			fail++
		} else {
			ok++
		}
		time.Sleep(40 * time.Millisecond)
	}
	return
}

// SendDocumentBytes uploads a file as a Telegram document.
func (c *Client) SendDocumentBytes(chatID int64, filename string, data []byte, caption string) error {
	if !c.enabled() {
		return fmt.Errorf("telegram token empty")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", fmt.Sprint(chatID))
	if caption != "" {
		_ = w.WriteField("caption", caption)
		_ = w.WriteField("parse_mode", "HTML")
	}
	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	_ = w.Close()
	req, err := http.NewRequest(http.MethodPost, apiBase+c.Token+"/sendDocument", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var ar apiResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return err
	}
	if !ar.OK {
		return fmt.Errorf("telegram: %s", ar.Description)
	}
	return nil
}

// SendPhotoBytes uploads PNG/JPEG bytes as a photo.
func (c *Client) SendPhotoBytes(chatID int64, filename string, data []byte, caption string) error {
	if !c.enabled() {
		return fmt.Errorf("telegram token empty")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", fmt.Sprint(chatID))
	if caption != "" {
		_ = w.WriteField("caption", caption)
		_ = w.WriteField("parse_mode", "HTML")
	}
	part, err := w.CreateFormFile("photo", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	_ = w.Close()
	req, err := http.NewRequest(http.MethodPost, apiBase+c.Token+"/sendPhoto", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var ar apiResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return err
	}
	if !ar.OK {
		return fmt.Errorf("telegram: %s", ar.Description)
	}
	return nil
}

// GetFileURL resolves a Telegram file_id to a downloadable HTTPS path for this bot.
func (c *Client) GetFileURL(fileID string) (string, error) {
	result, err := c.call("getFile", map[string]any{"file_id": fileID})
	if err != nil {
		return "", err
	}
	var f struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(result, &f); err != nil || f.FilePath == "" {
		return "", fmt.Errorf("file path missing")
	}
	return "https://api.telegram.org/file/bot" + c.Token + "/" + f.FilePath, nil
}

// DownloadFile fetches file bytes for a file_id (max ~10MB).
func (c *Client) DownloadFile(fileID string) ([]byte, string, error) {
	u, err := c.GetFileURL(fileID)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, "", err
	}
	name := "file.bin"
	if i := strings.LastIndex(u, "/"); i >= 0 && i+1 < len(u) {
		name = u[i+1:]
	}
	return data, name, nil
}

// SendPhotoFileID re-uses a file_id only valid for the same bot token.
func (c *Client) SendPhotoFileID(chatID int64, fileID, caption string) error {
	payload := map[string]any{
		"chat_id": chatID,
		"photo":   fileID,
	}
	if caption != "" {
		payload["caption"] = caption
		payload["parse_mode"] = "HTML"
	}
	_, err := c.call("sendPhoto", payload)
	return err
}

// GetChatMemberStatus returns Telegram chat member status for user in chat.
// chat can be @username or numeric id string. Status examples: creator, administrator, member, left, kicked, restricted.
func (c *Client) GetChatMemberStatus(chat string, userID int64) (string, error) {
	chat = strings.TrimSpace(chat)
	if chat == "" {
		return "", fmt.Errorf("empty chat")
	}
	payload := map[string]any{"chat_id": chat, "user_id": userID}
	// numeric chat ids as number when possible
	if len(chat) > 0 && (chat[0] == '-' || (chat[0] >= '0' && chat[0] <= '9')) {
		if n, err := strconv.ParseInt(chat, 10, 64); err == nil {
			payload["chat_id"] = n
		}
	}
	result, err := c.call("getChatMember", payload)
	if err != nil {
		return "", err
	}
	var m struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &m); err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(m.Status)), nil
}

// IsChatMember reports whether user is an active member of the chat/channel.
func (c *Client) IsChatMember(chat string, userID int64) (bool, string, error) {
	st, err := c.GetChatMemberStatus(chat, userID)
	if err != nil {
		return false, "", err
	}
	switch st {
	case "creator", "administrator", "member", "restricted":
		return true, st, nil
	default:
		return false, st, nil
	}
}
