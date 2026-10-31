package telegram

import (
	"fmt"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

const (
	MsgMaintenance  = "🛠 ربات در حال بروزرسانی می‌باشد.\nلطفاً دقایقی دیگر مجدداً تلاش کنید."
	MsgChannelNeed  = "برای استفاده از ربات باید در همه کانال‌های زیر عضو شوید."
	MsgChannelStill = "شما هنوز در همه کانال‌های الزامی عضو نیستید.\nپس از عضویت در همه، دکمه «✅ عضو شدم» را بزنید."
	MsgUpdatedAll   = "✅ ربات بروزرسانی شد و می‌توانید از خدمات استفاده کنید."
)

// UserHasActiveSubscription is true if linked client is enabled and not expired.
func UserHasActiveSubscription(db *gorm.DB, tgUserID int64) bool {
	var link models.TelegramCustomer
	if db.Where("telegram_user_id = ?", tgUserID).First(&link).Error != nil || link.ClientID == 0 {
		// try email pattern
		var cl models.Client
		if db.Where("email LIKE ? AND enable = ?", fmt.Sprintf("tg%d_%%", tgUserID), true).Order("id desc").First(&cl).Error != nil {
			return false
		}
		return !cl.IsExpired() && !cl.IsOverQuota()
	}
	var cl models.Client
	if db.First(&cl, link.ClientID).Error != nil {
		return false
	}
	return cl.Enable && !cl.IsExpired() && !cl.IsOverQuota()
}

// UserGate blocks when maintenance is on or multi-channel membership fails.
func UserGate(db *gorm.DB, cli *Client, tgUserID int64) (blocked bool, msg string, kb *InlineKeyboardMarkup) {
	if MaintenanceOn(db) {
		return true, MsgMaintenance, nil
	}
	if !ChannelEnforceOn(db) {
		return false, "", nil
	}
	channels := RequiredChannelList(db)
	if len(channels) == 0 || cli == nil {
		return false, "", nil
	}
	// Loyal customer exemption
	if ChannelExemptActive(db) && UserHasActiveSubscription(db, tgUserID) {
		return false, "", nil
	}
	missing := MissingChannels(cli, channels, tgUserID)
	if len(missing) == 0 {
		return false, "", nil
	}
	var sb strings.Builder
	sb.WriteString(MsgChannelNeed)
	sb.WriteString("\n\n")
	for _, ch := range missing {
		sb.WriteString("• <code>" + ch + "</code>\n")
	}
	sb.WriteString("\n" + MsgChannelStill)
	return true, sb.String(), channelJoinKeyboard(missing)
}

// MissingChannels lists channels the user is not a member of.
func MissingChannels(cli *Client, channels []string, tgUserID int64) []string {
	var missing []string
	for _, ch := range channels {
		ok, _, err := cli.IsChatMember(ch, tgUserID)
		if err != nil || !ok {
			missing = append(missing, ch)
		}
	}
	return missing
}

func channelJoinKeyboard(channels []string) *InlineKeyboardMarkup {
	rows := [][]InlineKeyboardButton{}
	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		url := ""
		if strings.HasPrefix(ch, "https://") {
			url = ch
		} else if strings.HasPrefix(ch, "@") {
			url = "https://t.me/" + strings.TrimPrefix(ch, "@")
		} else if len(ch) > 0 && ch[0] != '-' {
			url = "https://t.me/" + strings.TrimPrefix(ch, "@")
		}
		label := "📣 ورود: " + ch
		if len(label) > 40 {
			label = label[:37] + "…"
		}
		if url != "" {
			rows = append(rows, []InlineKeyboardButton{{Text: label, URL: url}})
		}
	}
	rows = append(rows, []InlineKeyboardButton{{Text: "✅ عضو شدم — بررسی عضویت", CallbackData: "check_join"}})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func FormatChannelLabel(ch string) string {
	ch = strings.TrimSpace(ch)
	if ch == "" {
		return "—"
	}
	return ch
}

// BroadcastAllCustomers sends text to every TelegramCustomer chat id.
func BroadcastAllCustomers(db *gorm.DB, text string) (ok, fail int) {
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return 0, 0
	}
	ids := CollectCustomerChatIDs(db, nil)
	return NewClient(token).Broadcast(ids, text)
}

func ChannelStatusText(db *gorm.DB) string {
	chs := RequiredChannelList(db)
	en := ChannelEnforceOn(db)
	st := "خاموش"
	if en {
		st = "روشن"
	}
	exempt := "خیر"
	if ChannelExemptActive(db) {
		exempt = "بله (مشتری فعال معاف)"
	}
	list := "—"
	if len(chs) > 0 {
		list = strings.Join(chs, ", ")
	}
	return fmt.Sprintf("📣 اجبار عضویت: <b>%s</b>\nکانال‌ها: <code>%s</code>\nمعافیت اشتراک فعال: <b>%s</b>\n(ربات کاربر باید ادمین همه کانال‌ها باشد)", st, list, exempt)
}

func MaintenanceStatusText(db *gorm.DB) string {
	if MaintenanceOn(db) {
		return "🛠 حالت بروزرسانی: <b>روشن</b>\nکاربران پیام «در حال بروزرسانی» می‌بینند."
	}
	return "🛠 حالت بروزرسانی: <b>خاموش</b>\nربات برای کاربران عادی فعال است."
}
