package telegram

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

func (b *UserBot) sendPortalLink(cli *Client, fromID, chatID int64) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil || link.SubID == "" {
		_ = cli.SendMessageMarkup(chatID, "ابتدا اشتراک یا تست بگیرید تا Sub ID داشته باشید.", b.mainMenu())
		return
	}
	base := portalPublicBase(b.DB)
	if base == "" {
		_ = cli.SendMessageMarkup(chatID, "آدرس عمومی پنل (public_host / subscription_base_url) تنظیم نشده.", b.mainMenu())
		return
	}
	tok, url := createPortalOneTime(b.DB, link.SubID, base)
	msg := fmt.Sprintf(`🖥 <b>پورتال وب کاربری</b>
بدون مینی‌اپ — در مرورگر کامپیوتر یا موبایل:

%s

Sub ID (ورود دستی):
<code>%s</code>
آدرس: %s/portal/

لینک یک‌بارمصرف حدود ۱۵ دقیقه اعتبار دارد.`, url, link.SubID, base)
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🖥 باز کردن پورتال وب", URL: url}},
		{{Text: "🏠 منو", CallbackData: "menu"}},
	}}
	_ = tok
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

func portalPublicBase(db *gorm.DB) string {
	base := strings.TrimRight(GetSetting(db, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(GetSetting(db, "public_host"), "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	return base
}

func createPortalOneTime(db *gorm.DB, subID, base string) (string, string) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", subID, time.Now().UnixNano())))
	tok := base64.RawURLEncoding.EncodeToString(sum[:16])
	exp := time.Now().Add(15 * time.Minute).Unix()
	key := "portal_ot_" + tok
	val := subID + "|" + strconv.FormatInt(exp, 10)
	var st models.PanelSetting
	if db.Where("key = ?", key).First(&st).Error == nil {
		_ = db.Model(&st).Update("value", val).Error
	} else {
		_ = db.Create(&models.PanelSetting{Key: key, Value: val}).Error
	}
	url := strings.TrimRight(base, "/") + "/portal/?token=" + tok
	return tok, url
}
