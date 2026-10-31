package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Optional runtime overrides (filled by panel when bots are configured).
var (
	overrideMu    sync.RWMutex
	overrideToken string
	overrideChats []string
)

// SetTelegramOverride allows the panel to push admin-bot credentials for alerts.
func SetTelegramOverride(token string, chatIDsCSV string) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	overrideToken = strings.TrimSpace(token)
	var chats []string
	for _, p := range strings.Split(chatIDsCSV, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			chats = append(chats, p)
		}
	}
	overrideChats = chats
}

func resolveCreds() (token string, chats []string) {
	token = os.Getenv("TELEGRAM_BOT_TOKEN")
	chat := os.Getenv("TELEGRAM_CHAT_ID")
	overrideMu.RLock()
	ot, oc := overrideToken, append([]string{}, overrideChats...)
	overrideMu.RUnlock()
	if token == "" {
		token = ot
	}
	if chat != "" {
		chats = append(chats, chat)
	}
	if len(chats) == 0 {
		chats = oc
	}
	return token, chats
}

func Telegram(message string) error {
	token, chats := resolveCreds()
	if token == "" || len(chats) == 0 {
		return fmt.Errorf("telegram is not configured")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	var last error
	ok := 0
	for _, chatID := range chats {
		form := url.Values{"chat_id": {chatID}, "text": {message}, "parse_mode": {"HTML"}}
		req, e := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(form.Encode()))
		if e != nil {
			last = e
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, e := client.Do(req)
		if e != nil {
			last = e
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				last = fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
				return
			}
			var out struct {
				OK bool `json:"ok"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			if !out.OK {
				last = fmt.Errorf("telegram rejected message")
				return
			}
			ok++
		}()
	}
	if ok == 0 {
		if last != nil {
			return last
		}
		return fmt.Errorf("telegram send failed")
	}
	return nil
}

// TelegramWithMarkup sends an HTML message with an inline keyboard.
// markup should be a map like {"inline_keyboard": [[{ "text": "...", "callback_data": "..." }]]}.
func TelegramWithMarkup(message string, markup any) error {
	token, chats := resolveCreds()
	if token == "" || len(chats) == 0 {
		return fmt.Errorf("telegram is not configured")
	}
	client := &http.Client{Timeout: 12 * time.Second}
	var last error
	ok := 0
	for _, chatID := range chats {
		body := map[string]any{
			"chat_id":      chatID,
			"text":         message,
			"parse_mode":   "HTML",
			"reply_markup": markup,
		}
		raw, err := json.Marshal(body)
		if err != nil {
			last = err
			continue
		}
		req, e := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(raw))
		if e != nil {
			last = e
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := client.Do(req)
		if e != nil {
			last = e
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				last = fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
				return
			}
			var out struct {
				OK bool `json:"ok"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			if !out.OK {
				last = fmt.Errorf("telegram rejected message")
				return
			}
			ok++
		}()
	}
	if ok == 0 {
		if last != nil {
			return last
		}
		return fmt.Errorf("telegram send failed")
	}
	return nil
}
