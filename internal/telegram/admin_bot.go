package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/backup"
	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/routing"
	"github.com/electruvpn/electruvpn/internal/shop"
	"gorm.io/gorm"
)

// AdminBot is an interactive Telegram bot for panel operators.
// Only Telegram user IDs listed in panel setting telegram_admin_ids may use it.
type AdminBot struct {
	DB                *gorm.DB
	Cfg               *config.Config
	AfterClientChange func() // regenerate xray after order approve
}

func (b *AdminBot) Run(ctx context.Context) {
	var cli *Client
	var lastToken string
	cmdsReady := false
	for {
		if ctx.Err() != nil {
			return
		}
		token := GetSetting(b.DB, KeyAdminToken)
		if token == "" {
			cli = nil
			lastToken = ""
			cmdsReady = false
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}
		if cli == nil || token != lastToken {
			cli = NewClient(token)
			lastToken = token
			cmdsReady = false
			_ = cli.DeleteWebhook()
		}
		if !cmdsReady {
			if err := cli.SetMyCommands([]BotCommand{
				{Command: "start", Description: "منوی اصلی ادمین"},
				{Command: "menu", Description: "نمایش منو"},
				{Command: "stats", Description: "گزارش لحظه‌ای"},
				{Command: "orders", Description: "سفارش‌های در انتظار"},
				{Command: "report", Description: "گزارش فروش"},
				{Command: "clients", Description: "خلاصه کلاینت‌ها"},
				{Command: "nodes", Description: "وضعیت نودها"},
				{Command: "tickets", Description: "تیکت‌های باز"},
				{Command: "backup", Description: "بکاپ پنل"},
				{Command: "help", Description: "راهنما"},
			}); err == nil {
				cmdsReady = true
			}
		}
		if err := b.pollOnce(ctx, cli); err != nil {
			slog.Warn("admin telegram bot", "error", err)
			if strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "409") {
				// Keep the same Client so the getUpdates offset survives (see UserBot.Run).
				if cli != nil {
					_ = cli.DeleteWebhook()
				}
				cmdsReady = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (b *AdminBot) isAdmin(fromID, chatID int64) bool {
	// Authorize the person who sent the update, not the chat. Matching on chatID
	// as well meant that registering a group's ID as an admin gave every member
	// of that group order-approval and backup powers.
	if fromID != 0 {
		return AdminRole(b.DB, fromID) != ""
	}
	return AdminRole(b.DB, chatID) != ""
}

func (b *AdminBot) roleOf(fromID int64) string {
	return AdminRole(b.DB, fromID)
}

func (b *AdminBot) pollOnce(ctx context.Context, cli *Client) error {
	updates, err := cli.GetUpdates(25)
	if err != nil {
		return err
	}
	for _, u := range updates {
		u := u
		safeHandle("admin bot", func() {
			if u.CallbackQuery != nil {
				b.onCallback(cli, u.CallbackQuery)
				return
			}
			if u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
				return
			}
			chatID := u.Message.Chat.ID
			fromID := chatID
			if u.Message.From != nil {
				fromID = u.Message.From.ID
			}
			if !b.isAdmin(fromID, chatID) {
				_ = cli.SendMessage(chatID, "⛔ دسترسی ندارید.\nآیدی عددی شما: <code>"+fmt.Sprint(fromID)+"</code>\nاین آیدی را در پنل → تلگرام → آیدی ادمین‌ها ثبت کنید.")
				return
			}
			text := strings.TrimSpace(u.Message.Text)
			if b.handleTicketReplyText(cli, fromID, chatID, text) {
				return
			}
			b.onText(cli, fromID, chatID, text)
		})
	}
	return nil
}

func (b *AdminBot) replyMainKeyboard(role string) *ReplyKeyboardMarkup {
	rows := [][]ReplyKeyboardButton{
		{{Text: "📊 گزارش"}, {Text: "📈 گزارش فروش"}, {Text: "🛒 سفارش‌ها"}},
		{{Text: "👥 کلاینت‌ها"}, {Text: "🔌 اینباندها"}, {Text: "🖥 نودها"}},
		{{Text: "🎫 تیکت‌ها"}, {Text: "👤 کاربر"}, {Text: "💾 بکاپ"}},
		{{Text: "🎁 هدیه"}, {Text: "🏷 پرومو"}, {Text: "🔗 معرف"}},
		{{Text: "📣 برودکست"}, {Text: "🛠 بروزرسانی"}, {Text: "📣 کانال"}},
		{{Text: "❓ راهنما"}, {Text: "🏠 منو"}},
	}
	if role == "finance" {
		rows = [][]ReplyKeyboardButton{
			{{Text: "📈 گزارش فروش"}, {Text: "🛒 سفارش‌ها"}, {Text: "📊 گزارش"}},
			{{Text: "❓ راهنما"}, {Text: "🏠 منو"}},
		}
	}
	if role == "support" {
		rows = [][]ReplyKeyboardButton{
			{{Text: "🎫 تیکت‌ها"}, {Text: "👤 کاربر"}},
			{{Text: "❓ راهنما"}, {Text: "🏠 منو"}},
		}
	}
	return &ReplyKeyboardMarkup{Keyboard: rows, IsPersistent: true, ResizeKeyboard: true, InputFieldPlaceholder: "منوی ادمین…"}
}

func adminPanelWebAppURL(db *gorm.DB) string {
	base := strings.TrimRight(GetSetting(db, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(GetSetting(db, "public_host"), "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/tma/admin/"
}

func adminMenuFor(role string, webAppURL string) *InlineKeyboardMarkup {
	webRow := []InlineKeyboardButton{}
	if webAppURL != "" && (role == "owner" || role == "admin" || role == "") {
		webRow = []InlineKeyboardButton{{Text: "🖥 پنل وب مدیریت", WebApp: &WebAppInfo{URL: webAppURL}}}
	}
	switch role {
	case "support":
		rows := [][]InlineKeyboardButton{
			{{Text: "🎫 تیکت‌ها", CallbackData: "adm:tickets"}},
			{{Text: "❓ راهنما", CallbackData: "adm:help"}},
		}
		return &InlineKeyboardMarkup{InlineKeyboard: rows}
	case "finance":
		rows := [][]InlineKeyboardButton{
			{{Text: "📈 گزارش فروش", CallbackData: "adm:report"}, {Text: "🛒 سفارش‌ها", CallbackData: "adm:orders"}},
			{{Text: "📊 گزارش لحظه‌ای", CallbackData: "adm:stats"}},
			{{Text: "❓ راهنما", CallbackData: "adm:help"}},
		}
		return &InlineKeyboardMarkup{InlineKeyboard: rows}
	default:
		rows := [][]InlineKeyboardButton{}
		if len(webRow) > 0 {
			rows = append(rows, webRow)
		}
		rows = append(rows,
			[]InlineKeyboardButton{{Text: "📊 گزارش لحظه‌ای", CallbackData: "adm:stats"}, {Text: "📈 گزارش فروش", CallbackData: "adm:report"}},
			[]InlineKeyboardButton{{Text: "🛒 سفارش‌ها", CallbackData: "adm:orders"}, {Text: "👤 مدیریت کاربر", CallbackData: "adm:users"}},
			[]InlineKeyboardButton{{Text: "👥 کلاینت‌ها", CallbackData: "adm:clients"}, {Text: "🔌 اینباندها", CallbackData: "adm:inbounds"}},
			[]InlineKeyboardButton{{Text: "🖥 نودها", CallbackData: "adm:nodes"}, {Text: "🎫 تیکت‌ها", CallbackData: "adm:tickets"}},
			[]InlineKeyboardButton{{Text: "💾 بکاپ پنل", CallbackData: "adm:backup"}, {Text: "📣 برودکست", CallbackData: "adm:bc"}},
			[]InlineKeyboardButton{{Text: "🎁 هدیه", CallbackData: "adm:gift"}, {Text: "🏷 پرومو", CallbackData: "adm:promo"}},
			[]InlineKeyboardButton{{Text: "🔗 معرف", CallbackData: "adm:referral"}, {Text: "📦 قیمت گیگ", CallbackData: "adm:customgb"}},
			[]InlineKeyboardButton{{Text: "🛠 حالت بروزرسانی", CallbackData: "adm:maint"}, {Text: "📣 کانال اجباری", CallbackData: "adm:channel"}},
			[]InlineKeyboardButton{{Text: "❓ راهنما", CallbackData: "adm:help"}},
		)
		return &InlineKeyboardMarkup{InlineKeyboard: rows}
	}
}

func (b *AdminBot) adminMenu(role string) *InlineKeyboardMarkup {
	return adminMenuFor(role, adminPanelWebAppURL(b.DB))
}

func adminMenu() *InlineKeyboardMarkup { return adminMenuFor("owner", "") }

func (b *AdminBot) onText(cli *Client, fromID, chatID int64, text string) {
	role := b.roleOf(fromID)
	// Map reply-keyboard labels to commands
	switch strings.TrimSpace(text) {
	case "🏠 منو", "منو":
		text = "/menu"
	case "📊 گزارش", "گزارش":
		text = "/stats"
	case "🛒 سفارش‌ها", "سفارش‌ها":
		text = "/orders"
	case "👥 کلاینت‌ها", "کلاینت‌ها":
		text = "/clients"
	case "🎫 تیکت‌ها", "تیکت‌ها":
		text = "/tickets"
	case "💾 بکاپ", "بکاپ":
		text = "/backup"
	case "📈 گزارش فروش":
		text = "/report"
	case "🔌 اینباندها", "اینباندها":
		text = "/inbounds"
	case "🖥 نودها", "نودها":
		text = "/nodes"
	case "👤 کاربر", "کاربر":
		text = "/users"
	case "📣 برودکست", "برودکست":
		text = "/bc_help"
	case "🎁 هدیه", "هدیه":
		text = "/gift_help"
	case "🏷 پرومو", "پرومو":
		text = "/promo_help"
	case "🔗 معرف", "معرف":
		text = "/referral"
	case "🛠 بروزرسانی", "بروزرسانی":
		text = "/maint"
	case "📣 کانال", "کانال":
		text = "/setchannel_help"
	case "❓ راهنما", "راهنما":
		text = "/help"
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		_ = cli.SendMessageMarkup(chatID, adminHelp(), b.adminMenu(role))
		return
	}
	cmd := strings.ToLower(parts[0])
	cmd = strings.TrimPrefix(cmd, "/")
	if i := strings.Index(cmd, "@"); i > 0 {
		cmd = cmd[:i]
	}
	switch cmd {
	case "start", "menu", "help":
		_ = cli.SendMessageFull(chatID, adminHelp()+"\nنقش شما: <b>"+role+"</b>", b.adminMenu(role), b.replyMainKeyboard(role))
	case "stats", "status":
		_ = cli.SendMessageMarkup(chatID, b.stats(), b.adminMenu(role))
	case "clients":
		_ = cli.SendMessageMarkup(chatID, b.clientsSummary(), b.adminMenu(role))
	case "inbounds":
		_ = cli.SendMessageMarkup(chatID, b.inboundsSummary(), b.adminMenu(role))
	case "nodes":
		_ = cli.SendMessageMarkup(chatID, b.nodesSummary(), b.adminMenu(role))
	case "tickets":
		if !RoleAllowed(role, "tickets") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط پشتیبان/مدیر", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, b.ticketsSummary(), b.adminMenu(role))
	case "backup":
		if !RoleAllowed(role, "backup") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, b.runBackupAndSend(cli, chatID), b.adminMenu(role))
	case "orders":
		if !RoleAllowed(role, "orders") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مالی/مدیر", b.adminMenu(role))
			return
		}
		b.sendOrders(cli, chatID)
	case "report":
		if !RoleAllowed(role, "report") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مالی/مدیر", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, ReportNow(b.DB), b.adminMenu(role))
	case "bc_help":
		if !RoleAllowed(role, "backup") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, broadcastHelp(), b.adminMenu(role))
	case "gift_help":
		if !RoleAllowed(role, "backup") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, giftAdminHelp(), b.adminMenu(role))
	case "promo_help":
		if !RoleAllowed(role, "backup") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, promoAdminHelp(), b.adminMenu(role))
	case "setchannel_help":
		if !RoleAllowed(role, "backup") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر", b.adminMenu(role))
			return
		}
		b.sendChannelPanel(cli, chatID, role)
	case "find", "search":
		if !RoleAllowed(role, "users") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		q := ""
		if len(parts) > 1 {
			q = strings.Join(parts[1:], " ")
		}
		_ = cli.SendMessageMarkup(chatID, b.searchUser(q), b.adminMenu(role))
	case "new":
		if !RoleAllowed(role, "users") {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, b.createQuickClient(parts), b.adminMenu(role))
	case "addgb":
		if len(parts) < 3 {
			_ = cli.SendMessage(chatID, "مثال: /addgb 12 20")
			return
		}
		id, _ := strconv.ParseUint(parts[1], 10, 64)
		gb, _ := strconv.ParseInt(parts[2], 10, 64)
		_ = cli.SendMessageMarkup(chatID, b.extendClient(uint(id), gb, 0), b.adminMenu(role))
	case "adddays":
		if len(parts) < 3 {
			_ = cli.SendMessage(chatID, "مثال: /adddays 12 30")
			return
		}
		id, _ := strconv.ParseUint(parts[1], 10, 64)
		days, _ := strconv.Atoi(parts[2])
		_ = cli.SendMessageMarkup(chatID, b.extendClient(uint(id), 0, days), b.adminMenu(role))
	case "enable":
		if len(parts) < 2 {
			return
		}
		id, _ := strconv.ParseUint(parts[1], 10, 64)
		_ = b.DB.Model(&models.Client{}).Where("id = ?", id).Updates(map[string]any{"enable": true, "subscription_status": "active"})
		_ = cli.SendMessage(chatID, "✅ فعال شد")
	case "disable":
		if len(parts) < 2 {
			return
		}
		id, _ := strconv.ParseUint(parts[1], 10, 64)
		_ = b.DB.Model(&models.Client{}).Where("id = ?", id).Updates(map[string]any{
			"enable": false, "subscription_status": "disabled",
		})
		_ = cli.SendMessage(chatID, "⛔ غیرفعال شد (status=disabled — تمدید خودکار انجام نمی‌شود)")
	case "users", "manage":
		_ = cli.SendMessageMarkup(chatID, b.manageHelp(), b.adminMenu(role))
	case "backupcode", "cloudcode", "usercode":
		if role != "owner" && role != "admin" && role != "support" {
			_ = cli.SendMessageMarkup(chatID, "⛔ دسترسی ندارید", b.adminMenu(role))
			return
		}
		q := ""
		if len(parts) > 1 {
			q = strings.Join(parts[1:], " ")
		}
		_ = cli.SendMessageMarkup(chatID, AdminFormatCloudBackups(b.DB, q), b.adminMenu(role))
	case "issuebackup":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		if len(parts) < 2 {
			_ = cli.SendMessage(chatID, "مثال: /issuebackup 123456789")
			return
		}
		tgID, _ := ResolveTelegramUserID(b.DB, parts[1])
		if tgID == 0 {
			_ = cli.SendMessage(chatID, "کاربر پیدا نشد")
			return
		}
		code, sum, err := AdminIssueBackup(b.DB, tgID)
		if err != nil {
			_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.adminMenu(role))
			return
		}
		msg := fmt.Sprintf("✅ کد جدید برای TG <code>%d</code>:\n<code>%s</code>\n\n%s\n\nبا /sendbackup %d می‌توانید همین کد را برای کاربر بفرستید.", tgID, code, sum, tgID)
		_ = cli.SendMessageMarkup(chatID, msg, b.adminMenu(role))
	case "sendbackup":
		if role != "owner" && role != "admin" && role != "support" {
			_ = cli.SendMessageMarkup(chatID, "⛔ دسترسی ندارید", b.adminMenu(role))
			return
		}
		if len(parts) < 2 {
			_ = cli.SendMessage(chatID, "مثال: /sendbackup 123456789")
			return
		}
		tgID, _ := ResolveTelegramUserID(b.DB, parts[1])
		if tgID == 0 {
			_ = cli.SendMessage(chatID, "کاربر پیدا نشد")
			return
		}
		info := GetActiveCloudBackup(b.DB, tgID)
		rows := AdminListCloudBackups(b.DB, tgID, 1)
		if len(rows) == 0 || !rows[0].Enabled {
			_ = cli.SendMessage(chatID, "کد فعال نیست. ابتدا /issuebackup "+fmt.Sprint(tgID))
			return
		}
		code := rows[0].BackupCode
		ut := GetSetting(b.DB, KeyUserToken)
		if ut == "" {
			_ = cli.SendMessage(chatID, "توکن ربات کاربر تنظیم نشده — کد را دستی بدهید:\n<code>"+code+"</code>")
			return
		}
		chatTo := tgID
		var link models.TelegramCustomer
		if b.DB.Where("telegram_user_id = ?", tgID).First(&link).Error == nil && link.TelegramChatID != 0 {
			chatTo = link.TelegramChatID
		}
		body := "☁️ <b>کد پشتیبان ابری شما</b> (از پشتیبانی)\n\n<code>" + code + "</code>\n\n"
		body += "⚠️ این کد را در جای امن نگه دارید.\n"
		if info.DaysLeft > 0 {
			body += fmt.Sprintf("⏱ حدود %d روز اعتبار دارد.\n", info.DaysLeft)
		}
		body += "برای بازیابی در تلگرام جدید: منو → ♻️ بازیابی اکانت"
		if err := NewClient(ut).SendMessage(chatTo, body); err != nil {
			_ = cli.SendMessage(chatID, "ارسال ناموفق: "+err.Error()+"\nکد: <code>"+code+"</code>")
			return
		}
		_ = cli.SendMessageMarkup(chatID, "✅ کد برای کاربر ارسال شد.\n<code>"+code+"</code>", b.adminMenu(role))
	case "reenablebackup":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		if len(parts) < 2 {
			_ = cli.SendMessage(chatID, "مثال: /reenablebackup EV-AB12-CD34EF")
			return
		}
		row, err := AdminReenableBackup(b.DB, parts[1])
		if err != nil {
			_ = cli.SendMessage(chatID, "❌ "+err.Error())
			return
		}
		_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ کد دوباره فعال شد (۳۰ روز).\n<code>%s</code>\nصاحب: TG <code>%d</code>", row.BackupCode, row.OwnerTelegramID), b.adminMenu(role))
	case "giftissue", "issuegift", "giftrevoke", "revokegift", "gifts", "listgifts", "setloyalty", "setprorate":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, AdminGiftCommand(b.DB, cmd, parts), b.adminMenu(role))
	case "addpromo", "promo", "promos", "disablepromo", "listpromo":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, AdminPromoCommand(b.DB, cmd, parts), b.adminMenu(role))
	case "referral", "setreferral", "affiliate":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		if cmd == "referral" && len(parts) < 2 {
			_ = cli.SendMessageMarkup(chatID, AdminReferralStatus(b.DB), b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, AdminSetReferral(b.DB, parts), b.adminMenu(role))
	case "maint", "maintenance", "update_mode":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		b.sendMaintPanel(cli, chatID, role)
	case "channel", "force_channel":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		b.sendChannelPanel(cli, chatID, role)
	case "setchannel":
		if role != "owner" && role != "admin" {
			return
		}
		if len(parts) < 2 {
			_ = cli.SendMessage(chatID, "مثال چندکاناله:\n/setchannel @ch1,@ch2,-100123\nیا تکی: /setchannel @mychannel")
			return
		}
		raw := strings.Join(parts[1:], " ")
		raw = strings.ReplaceAll(raw, " ", "")
		_ = SetSetting(b.DB, KeyRequiredChannels, raw)
		_ = SetSetting(b.DB, KeyRequiredChannel, strings.Split(raw, ",")[0])
		_ = SetSetting(b.DB, KeyChannelEnforce, "1")
		_ = cli.SendMessageMarkup(chatID, "✅ کانال‌های اجباری:\n<code>"+raw+"</code>\nربات کاربر را ادمین همه کانال‌ها کنید.", b.adminMenu(role))
	case "setgbprice":
		if role != "owner" && role != "admin" {
			return
		}
		if len(parts) < 2 {
			_ = cli.SendMessage(chatID, "مثال:\n/setgbprice 15000\nقیمت هر گیگ به تومان (ماهانه)\n/customgb on|off\n/customgb min 5 max 200 days 30")
			return
		}
		_ = SetSetting(b.DB, models.CustomGBPriceKey, parts[1])
		_ = SetSetting(b.DB, models.CustomGBEnabledKey, "1")
		_ = cli.SendMessageMarkup(chatID, "✅ قیمت هر گیگ: <b>"+parts[1]+"</b> تومان — خرید حجم دلخواه فعال شد.", b.adminMenu(role))
	case "customgb":
		if role != "owner" && role != "admin" {
			return
		}
		if len(parts) < 2 {
			cfg := shop.LoadCustomGBConfig(b.DB)
			_ = cli.SendMessage(chatID, fmt.Sprintf("حجم دلخواه: enabled=%v price=%d min=%d max=%d days=%d\n/customgb on|off\n/setgbprice 15000", cfg.Enabled, cfg.PricePerGB, cfg.MinGB, cfg.MaxGB, cfg.Days))
			return
		}
		switch strings.ToLower(parts[1]) {
		case "on", "1", "true":
			_ = SetSetting(b.DB, models.CustomGBEnabledKey, "1")
			_ = cli.SendMessage(chatID, "✅ خرید حجم دلخواه روشن")
		case "off", "0", "false":
			_ = SetSetting(b.DB, models.CustomGBEnabledKey, "0")
			_ = cli.SendMessage(chatID, "حجم دلخواه خاموش")
		case "min":
			if len(parts) >= 3 {
				_ = SetSetting(b.DB, models.CustomGBMinKey, parts[2])
			}
			_ = cli.SendMessage(chatID, "OK")
		case "max":
			if len(parts) >= 3 {
				_ = SetSetting(b.DB, models.CustomGBMaxKey, parts[2])
			}
			_ = cli.SendMessage(chatID, "OK")
		case "days":
			if len(parts) >= 3 {
				_ = SetSetting(b.DB, models.CustomGBDaysKey, parts[2])
			}
			_ = cli.SendMessage(chatID, "OK")
		default:
			_ = cli.SendMessage(chatID, "on|off|min|max|days")
		}
	case "broadcast", "bc":
		if role != "owner" && role != "admin" {
			_ = cli.SendMessageMarkup(chatID, "⛔ فقط مدیر کل", b.adminMenu(role))
			return
		}
		// /broadcast <target> <message...>
		if len(parts) < 3 {
			_ = cli.SendMessage(chatID, `📣 برودکست هدفمند:
/broadcast all متن پیام
/broadcast expired متن
/broadcast active متن
/broadcast expiring_3d متن
/broadcast purchased_over_30d متن
/broadcast no_purchase متن
/broadcast trial متن
/broadcast high_usage متن`)
			return
		}
		target := strings.ToLower(parts[1])
		msg := strings.Join(parts[2:], " ")
		matched, ok, fail := TargetedBroadcast(b.DB, target, msg)
		_ = cli.SendMessageMarkup(chatID, fmt.Sprintf(
			"📣 برودکست «%s»\nمخاطب الگوریتم: <b>%d</b>\nارسال موفق: %d · ناموفق: %d",
			target, matched, ok, fail), b.adminMenu(role))
	case "bcpreview", "broadcast_preview":
		if role != "owner" && role != "admin" {
			return
		}
		p := BroadcastPreview(b.DB)
		_ = cli.SendMessageMarkup(chatID, fmt.Sprintf(
			"📊 <b>پیش‌نمایش مخاطبان برودکست</b>\n\nهمه: <b>%d</b>\nفعال: <b>%d</b>\nمنقضی: <b>%d</b>\nبدون خرید: <b>%d</b>\n\nارسال: /broadcast active|expired|no_purchase|all متن",
			p["all"], p["active"], p["expired"], p["no_purchase"]), b.adminMenu(role))
	default:
		if strings.HasPrefix(cmd, "approve_") {
			if !RoleAllowed(role, "orders") {
				_ = cli.SendMessageMarkup(chatID, "⛔ فقط مالی/مدیر", b.adminMenu(role))
				return
			}
			id, _ := strconv.ParseUint(strings.TrimPrefix(cmd, "approve_"), 10, 64)
			_ = cli.SendMessageMarkup(chatID, b.approveOrder(uint(id)), b.adminMenu(role))
			return
		}
		if strings.HasPrefix(cmd, "reject_") {
			if !RoleAllowed(role, "orders") {
				_ = cli.SendMessageMarkup(chatID, "⛔ فقط مالی/مدیر", b.adminMenu(role))
				return
			}
			id, _ := strconv.ParseUint(strings.TrimPrefix(cmd, "reject_"), 10, 64)
			_ = cli.SendMessageMarkup(chatID, b.rejectOrder(uint(id)), b.adminMenu(role))
			return
		}
		_ = cli.SendMessageMarkup(chatID, "دستور ناشناخته.\n\n"+adminHelp(), b.adminMenu(role))
	}
}

func (b *AdminBot) onCallback(cli *Client, cq *CallbackQuery) {
	chatID := int64(0)
	fromID := int64(0)
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
	}
	if cq.From != nil {
		fromID = cq.From.ID
		if chatID == 0 {
			chatID = fromID
		}
	}
	_ = cli.AnswerCallback(cq.ID, "")
	if !b.isAdmin(fromID, chatID) {
		_ = cli.SendMessage(chatID, "⛔ دسترسی ندارید. آیدی: <code>"+fmt.Sprint(fromID)+"</code>")
		return
	}
	role := b.roleOf(fromID)
	data := strings.TrimSpace(cq.Data)
	// map callback to permission action
	action := "menu"
	switch {
	case data == "adm:orders" || strings.HasPrefix(data, "adm:ok:") || strings.HasPrefix(data, "adm:no:"):
		action = "orders"
	case data == "adm:report" || data == "adm:stats":
		action = "report"
	case data == "adm:tickets" || strings.HasPrefix(data, "adm:treply:") || strings.HasPrefix(data, "adm:tclose:"):
		action = "tickets"
	case data == "adm:backup":
		action = "backup"
	case data == "adm:users" || data == "adm:clients" || data == "adm:inbounds" || data == "adm:nodes":
		action = "users"
	case data == "adm:maint" || data == "adm:maint_on" || data == "adm:maint_off" || data == "adm:channel" || data == "adm:channel_on" || data == "adm:channel_off" || data == "adm:bc" || data == "adm:gift" || data == "adm:promo" || data == "adm:referral" || data == "adm:customgb" || data == "adm:exempt_on" || data == "adm:exempt_off":
		action = "backup" // owner-level
	case strings.HasPrefix(data, "fo:"):
		action = "nodes" // failover confirm is node-ops
	}
	if !RoleAllowed(role, action) {
		_ = cli.SendMessageMarkup(chatID, "⛔ این بخش برای نقش شما مجاز نیست ("+role+").", b.adminMenu(role))
		return
	}
	switch {
	case data == "adm:stats":
		_ = cli.SendMessageMarkup(chatID, b.stats(), b.adminMenu(role))
	case data == "adm:orders":
		b.sendOrders(cli, chatID)
	case data == "adm:clients":
		_ = cli.SendMessageMarkup(chatID, b.clientsSummary()+"\n\nجستجو: <code>/find آیدی یا ایمیل</code>\nفعال/غیرفعال: <code>/enable ID</code> · <code>/disable ID</code>\nحجم/روز: <code>/addgb ID 10</code> · <code>/adddays ID 30</code>", &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "🛒 سفارش‌ها", CallbackData: "adm:orders"}, {Text: "👤 مدیریت کاربر", CallbackData: "adm:users"}},
			{{Text: "◀️ منو", CallbackData: "adm:help"}},
		}})
	case data == "adm:inbounds":
		_ = cli.SendMessageMarkup(chatID, b.inboundsSummary(), b.adminMenu(role))
	case data == "adm:nodes":
		_ = cli.SendMessageMarkup(chatID, b.nodesSummary(), b.adminMenu(role))
	case data == "adm:tickets":
		_ = cli.SendMessageMarkup(chatID, b.ticketsSummary(), b.adminMenu(role))
	case data == "adm:backup":
		_ = cli.SendMessageMarkup(chatID, b.runBackupAndSend(cli, chatID), b.adminMenu(role))
	case data == "adm:help":
		_ = cli.SendMessageMarkup(chatID, adminHelp(), b.adminMenu(role))
	case data == "adm:report":
		_ = cli.SendMessageMarkup(chatID, ReportNow(b.DB), b.adminMenu(role))
	case data == "adm:users":
		_ = cli.SendMessageMarkup(chatID, b.manageHelp(), &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "🛒 سفارش‌ها", CallbackData: "adm:orders"}, {Text: "👥 کلاینت‌ها", CallbackData: "adm:clients"}},
			{{Text: "🎫 تیکت‌ها", CallbackData: "adm:tickets"}, {Text: "📊 گزارش", CallbackData: "adm:stats"}},
			{{Text: "◀️ منوی اصلی", CallbackData: "adm:help"}},
		}})
	case data == "adm:maint":
		b.sendMaintPanel(cli, chatID, role)
	case data == "adm:maint_on":
		_ = SetSetting(b.DB, KeyBotMaintenance, "1")
		AuditTelegram(b.DB, fromID, role, "tg.maint.on", "bot", nil)
		_ = cli.SendMessageMarkup(chatID, "✅ حالت بروزرسانی روشن شد.\nکاربران پیام محدودیت می‌بینند.", b.adminMenu(role))
	case data == "adm:maint_off":
		was := MaintenanceOn(b.DB)
		_ = SetSetting(b.DB, KeyBotMaintenance, "0")
		AuditTelegram(b.DB, fromID, role, "tg.maint.off", "bot", nil)
		ok, fail := 0, 0
		if was {
			_ = cli.SendMessage(chatID, "⏳ ارسال اطلاع به کاربران…")
			ok, fail = BroadcastAllCustomers(b.DB, MsgUpdatedAll)
		}
		_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ بروزرسانی تمام شد.\nاطلاع‌رسانی → موفق %d · ناموفق %d", ok, fail), b.adminMenu(role))
	case data == "adm:channel":
		b.sendChannelPanel(cli, chatID, role)
	case data == "adm:channel_on":
		if RequiredChannel(b.DB) == "" {
			_ = cli.SendMessage(chatID, "اول با /setchannel @name کانال را تنظیم کنید.")
		} else {
			_ = SetSetting(b.DB, KeyChannelEnforce, "1")
			_ = cli.SendMessageMarkup(chatID, "✅ اجبار عضویت روشن شد.", b.adminMenu(role))
		}
	case data == "adm:channel_off":
		_ = SetSetting(b.DB, KeyChannelEnforce, "0")
		_ = cli.SendMessageMarkup(chatID, "✅ اجبار عضویت خاموش شد.", b.adminMenu(role))
	case strings.HasPrefix(data, "adm:treply:"):
		if tid, ok := parseTicketCB(data, "adm:treply:"); ok {
			b.startTicketReply(cli, fromID, chatID, tid)
		}
	case strings.HasPrefix(data, "adm:tclose:"):
		if tid, ok := parseTicketCB(data, "adm:tclose:"); ok {
			b.closeTicketCmd(cli, chatID, tid)
		}
	case strings.HasPrefix(data, "adm:ok:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "adm:ok:"), 10, 64)
		_ = cli.SendMessageMarkup(chatID, b.approveOrder(uint(id)), b.adminMenu(role))
		b.sendOrders(cli, chatID)
	case strings.HasPrefix(data, "adm:no:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "adm:no:"), 10, 64)
		_ = cli.SendMessageMarkup(chatID, b.rejectOrder(uint(id)), b.adminMenu(role))
		b.sendOrders(cli, chatID)
	// --- Confirm-before-Failover callbacks (fo:yes / fo:no / fo:pick) ---
	case strings.HasPrefix(data, "fo:yes:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "fo:yes:"), 10, 64)
		changed, msg := routing.ApplyConfirmedFailover(b.DB, uint(id), "")
		_ = cli.SendMessageMarkup(chatID, msg, b.adminMenu(role))
		if changed {
			_ = cli.SendMessage(chatID, "♻️ Failover تأیید شد. کانفیگ در چرخه بعدی سلامت‌سنجی بازسازی می‌شود.")
		}
	case strings.HasPrefix(data, "fo:no:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "fo:no:"), 10, 64)
		msg := routing.DenyFailover(uint(id))
		_ = cli.SendMessageMarkup(chatID, msg, b.adminMenu(role))
	case strings.HasPrefix(data, "fo:pick:"):
		// format: fo:pick:<chainID>:<host>
		rest := strings.TrimPrefix(data, "fo:pick:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			_ = cli.SendMessage(chatID, "فرمت callback نامعتبر")
			return
		}
		id, _ := strconv.ParseUint(parts[0], 10, 64)
		host := parts[1]
		changed, msg := routing.ApplyConfirmedFailover(b.DB, uint(id), host)
		_ = cli.SendMessageMarkup(chatID, msg, b.adminMenu(role))
		if changed {
			_ = cli.SendMessage(chatID, "♻️ Failover دستی تأیید شد. کانفیگ در چرخه بعدی سلامت‌سنجی بازسازی می‌شود.")
		}
	case data == "adm:bc":
		_ = cli.SendMessageMarkup(chatID, broadcastHelp(), b.adminMenu(role))
	case data == "adm:gift":
		_ = cli.SendMessageMarkup(chatID, giftAdminHelp(), b.adminMenu(role))
	case data == "adm:promo":
		_ = cli.SendMessageMarkup(chatID, promoAdminHelp(), b.adminMenu(role))
	case data == "adm:referral":
		_ = cli.SendMessageMarkup(chatID, AdminReferralStatus(b.DB)+"\n\n"+referralAdminHelp(), b.adminMenu(role))
	case data == "adm:customgb":
		_ = cli.SendMessageMarkup(chatID, customGBAdminHelp(b.DB), b.adminMenu(role))
	case data == "adm:exempt_on":
		_ = SetSetting(b.DB, KeyChannelExemptActive, "1")
		_ = cli.SendMessageMarkup(chatID, "✅ معافیت عضویت کانال برای مشتریان فعال روشن شد.", b.adminMenu(role))
	case data == "adm:exempt_off":
		_ = SetSetting(b.DB, KeyChannelExemptActive, "0")
		_ = cli.SendMessageMarkup(chatID, "✅ معافیت عضویت کانال خاموش شد.", b.adminMenu(role))
	default:
		_ = cli.SendMessageMarkup(chatID, adminHelp(), b.adminMenu(role))
	}
}

func broadcastHelp() string {
	return `<b>📣 برودکست هدفمند</b>
دستور:
<code>/bc all متن پیام</code>
<code>/bc active متن</code>
<code>/bc expired متن</code>
<code>/bc no_purchase متن</code>

هدف‌ها: all · active · expired · no_purchase`
}

func giftAdminHelp() string {
	return `<b>🎁 کارت هدیه</b>
<code>/gift create پلن_آیدی تعداد</code>
<code>/gift list</code>
<code>/gift disable کد</code>`
}

func promoAdminHelp() string {
	return `<b>🏷 کد تخفیف</b>
<code>/promo create کد درصد سقف</code>
<code>/promo list</code>
<code>/promo off کد</code>`
}

func referralAdminHelp() string {
	return `تنظیم:
<code>/setreferral 10</code> — درصد پاداش
<code>/setreferral off</code> — خاموش`
}

func customGBAdminHelp(db *gorm.DB) string {
	price := GetSetting(db, "shop_custom_gb_price")
	if price == "" {
		price = "—"
	}
	return fmt.Sprintf(`<b>📦 حجم دلخواه</b>
قیمت فعلی هر گیگ: <b>%s</b> تومان

تنظیم:
<code>/setcustomgb 50000</code>
<code>/setcustomgb off</code>`, price)
}

func adminHelp() string {
	return `<b>🤖 ربات ادمین ElectruVPN</b>

فقط آیدی‌های عددی تعریف‌شده در پنل مجازند.

دکمه‌ها یا دستورات:
🖥 پنل وب مدیریت — داشبورد گرافیکی داخل تلگرام (WebApp)
/stats — گزارش لحظه‌ای
/orders — سفارش‌های در انتظار تأیید
/clients /inbounds /nodes /tickets
/backup — پشتیبان‌گیری کامل پنل + ارسال فایل
/approve_ID — تأیید سفارش
/reject_ID — رد سفارش
/find /new /addgb /adddays — مدیریت کاربر
/backupcode آیدی — پیدا کردن کد پشتیبان کاربر
/issuebackup آیدی — صدور کد جدید برای کاربر
/sendbackup آیدی — ارسال کد فعال به کاربر
/reenablebackup کد — فعال‌سازی مجدد کد
/referral — وضعیت سیستم معرف
/setreferral 10 — درصد پاداش معرف (۰–۵۰)
/setreferral off — خاموش کردن معرف
/report — گزارش فروش ۲۴ساعته
/maint — حالت بروزرسانی ربات
/setchannel @ch — کانال اجباری

آیدی ادمین را از @userinfobot بگیرید و در:
پنل → تلگرام → آیدی عددی ادمین‌ها
ثبت کنید.`
}

func (b *AdminBot) sendOrders(cli *Client, chatID int64) {
	var rows []models.ShopOrder
	b.DB.Where("status = ?", "pending_review").Order("id asc").Limit(15).Find(&rows)
	if len(rows) == 0 {
		_ = cli.SendMessageMarkup(chatID, "✅ سفارش در انتظار تأیید نیست.", b.adminMenu("owner"))
		return
	}
	_ = cli.SendMessage(chatID, fmt.Sprintf("🛒 <b>%d سفارش در انتظار تأیید</b>\nبا دکمه‌ها تأیید یا رد کنید:", len(rows)))
	for _, o := range rows {
		kind := o.PlanName
		if o.Kind == "wallet_topup" {
			kind = "شارژ کیف پول"
		}
		text := fmt.Sprintf("🧾 سفارش <b>#%d</b>\n%s\nمبلغ: <b>%s تومان</b>\nکاربر: %s\nTG ID: <code>%d</code>\nوضعیت: %s",
			o.ID, kind, formatToman(o.PriceToman), o.TelegramName, o.TelegramUserID, o.Status)
		kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ تأیید", CallbackData: fmt.Sprintf("adm:ok:%d", o.ID)},
				{Text: "❌ رد", CallbackData: fmt.Sprintf("adm:no:%d", o.ID)},
			},
		}}
		_ = cli.SendMessageMarkup(chatID, text, kb)
	}
	_ = cli.SendMessageMarkup(chatID, "منوی ادمین:", b.adminMenu("owner"))
}

func (b *AdminBot) stats() string {
	var users, clients, enabled, inbounds, tickets, pendingOrders int64
	b.DB.Model(&models.User{}).Count(&users)
	b.DB.Model(&models.Client{}).Count(&clients)
	b.DB.Model(&models.Client{}).Where("enable = ?", true).Count(&enabled)
	b.DB.Model(&models.Inbound{}).Count(&inbounds)
	b.DB.Model(&models.Ticket{}).Where("status <> ?", "closed").Count(&tickets)
	b.DB.Model(&models.ShopOrder{}).Where("status = ?", "pending_review").Count(&pendingOrders)
	var traffic struct{ Up, Down int64 }
	b.DB.Model(&models.Client{}).Select("COALESCE(SUM(up),0) as up, COALESCE(SUM(down),0) as down").Scan(&traffic)
	return fmt.Sprintf(`📊 <b>گزارش لحظه‌ای</b>
ادمین‌های پنل: %d
کلاینت‌ها: %d (فعال %d)
اینباند: %d
تیکت باز: %d
سفارش در انتظار: <b>%d</b>
ترافیک Σ: ↑ %s  ↓ %s
%s UTC`,
		users, clients, enabled, inbounds, tickets, pendingOrders,
		humanBytes(traffic.Up), humanBytes(traffic.Down),
		time.Now().UTC().Format("2006-01-02 15:04"))
}

func (b *AdminBot) clientsSummary() string {
	var total, active, expired, over int64
	b.DB.Model(&models.Client{}).Count(&total)
	b.DB.Model(&models.Client{}).Where("enable = ?", true).Count(&active)
	now := time.Now().UnixMilli()
	b.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ?", now).Count(&expired)
	var rows []models.Client
	b.DB.Select("total_gb, up, down").Find(&rows)
	for _, r := range rows {
		if r.IsOverQuota() {
			over++
		}
	}
	return fmt.Sprintf("👥 کلاینت‌ها\nکل: %d\nفعال: %d\nمنقضی: %d\nاتمام حجم: %d", total, active, expired, over)
}

func (b *AdminBot) inboundsSummary() string {
	var rows []models.Inbound
	b.DB.Order("id asc").Limit(20).Find(&rows)
	if len(rows) == 0 {
		return "اینباندی نیست."
	}
	var sb strings.Builder
	sb.WriteString("🔌 <b>اینباندها</b>\n")
	for _, ib := range rows {
		st := "off"
		if ib.Enable {
			st = "on"
		}
		sb.WriteString(fmt.Sprintf("• #%d %s · %s/%s · :%d · %s\n", ib.ID, ib.Remark, ib.Protocol, ib.Network, ib.Port, st))
	}
	return sb.String()
}

func (b *AdminBot) nodesSummary() string {
	var rows []models.ServerNode
	b.DB.Order("id asc").Limit(20).Find(&rows)
	if len(rows) == 0 {
		return "نودی ثبت نشده."
	}
	var sb strings.Builder
	sb.WriteString("🖥 <b>نودها</b>\n")
	for _, n := range rows {
		st := "disabled"
		if n.Enabled {
			st = n.DrainState
			if st == "" {
				st = "active"
			}
		}
		sb.WriteString(fmt.Sprintf("• %s · %s · %s\n", n.Name, n.Address, st))
	}
	return sb.String()
}

func (b *AdminBot) ticketsSummary() string {
	var open, pending int64
	b.DB.Model(&models.Ticket{}).Where("status = ?", "open").Count(&open)
	b.DB.Model(&models.Ticket{}).Where("status = ?", "pending").Count(&pending)
	var latest []models.Ticket
	b.DB.Where("status <> ?", "closed").Order("updated_at desc").Limit(5).Find(&latest)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎫 تیکت‌ها\nباز: %d · در انتظار: %d\n", open, pending))
	for _, t := range latest {
		sb.WriteString(fmt.Sprintf("• #%d %s [%s] — %s\n", t.ID, t.Subject, t.Status, t.Email))
	}
	return sb.String()
}

func (b *AdminBot) runBackup() string {
	if b.Cfg == nil {
		return "Backup unavailable (no config)."
	}
	dir := filepath.Join(b.Cfg.DataDir(), "backups")
	row, err := backup.Create(b.DB, b.Cfg.DBPath, b.Cfg.XrayConfigPath, b.Cfg.SingBoxConfigPath, dir, b.Cfg.BackupRemoteDir)
	if err != nil {
		return "❌ بکاپ ناموفق: " + err.Error()
	}
	return fmt.Sprintf("✅ بکاپ ساخته شد\nPath: <code>%s</code>\nSize: %s\n\nاز پنل → Backups می‌توانید Restore کنید.", row.Path, humanBytes(row.Size))
}

// runBackupAndSend creates a full panel backup and delivers the ZIP to the admin chat.
func (b *AdminBot) runBackupAndSend(cli *Client, chatID int64) string {
	if b.Cfg == nil {
		return "Backup unavailable (no config)."
	}
	_ = cli.SendMessage(chatID, "⏳ در حال تهیه نسخه پشتیبان کامل پنل (دیتابیس + کانفیگ‌ها + تنظیمات)…")
	dir := filepath.Join(b.Cfg.DataDir(), "backups")
	row, err := backup.Create(b.DB, b.Cfg.DBPath, b.Cfg.XrayConfigPath, b.Cfg.SingBoxConfigPath, dir, b.Cfg.BackupRemoteDir)
	if err != nil {
		return "❌ بکاپ ناموفق: " + err.Error()
	}
	data, err := os.ReadFile(row.Path)
	if err != nil {
		return fmt.Sprintf("✅ بکاپ روی سرور ذخیره شد ولی خواندن فایل ممکن نشد.\n<code>%s</code>\n%s", row.Path, humanBytes(row.Size))
	}
	// Telegram bot API hard limit ~50MB for bots
	const maxSend = 48 << 20
	caption := fmt.Sprintf("💾 <b>بکاپ کامل پنل</b>\nحجم: %s\nمسیر سرور: <code>%s</code>\n\nبرای بازگردانی: پنل وب → بخش Backups → Restore", humanBytes(row.Size), row.Path)
	if len(data) <= maxSend {
		name := filepath.Base(row.Path)
		if err := cli.SendDocumentBytes(chatID, name, data, caption); err != nil {
			return fmt.Sprintf("✅ بکاپ ذخیره شد (ارسال تلگرام ناموفق: %v)\n<code>%s</code>\n%s", err, row.Path, humanBytes(row.Size))
		}
		return fmt.Sprintf("✅ بکاپ کامل آماده و فایل برای شما ارسال شد.\nحجم: %s\nدر پنل: Backups → Restore", humanBytes(row.Size))
	}
	return fmt.Sprintf("✅ بکاپ ذخیره شد (فایل بزرگ‌تر از حد تلگرام است — از پنل دانلود کنید).\n<code>%s</code>\n%s", row.Path, humanBytes(row.Size))
}

func (b *AdminBot) sendMaintPanel(cli *Client, chatID int64, role string) {
	txt := MaintenanceStatusText(b.DB) + "\n\nبا روشن کردن، کاربران در ربات و مینی‌اپ پیام «در حال بروزرسانی» می‌بینند.\nبا خاموش کردن، به همه کاربران پیام اتمام بروزرسانی ارسال می‌شود."
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🔴 روشن کردن بروزرسانی", CallbackData: "adm:maint_on"}},
		{{Text: "🟢 خاموش کردن + اطلاع به همه", CallbackData: "adm:maint_off"}},
		{{Text: "◀️ منو", CallbackData: "adm:help"}},
	}}
	_ = cli.SendMessageMarkup(chatID, txt, kb)
}

func (b *AdminBot) sendChannelPanel(cli *Client, chatID int64, role string) {
	txt := ChannelStatusText(b.DB) + "\n\nچندکاناله:\n<code>/setchannel @ch1,@ch2</code>\nربات کاربر را ادمین همه کانال‌ها کنید.\nبرودکست: /broadcast expired متن"
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "✅ اجبار عضویت روشن", CallbackData: "adm:channel_on"}},
		{{Text: "⛔ اجبار عضویت خاموش", CallbackData: "adm:channel_off"}},
		{{Text: "🎁 معافیت مشتری فعال: روشن", CallbackData: "adm:exempt_on"}},
		{{Text: "معافیت مشتری فعال: خاموش", CallbackData: "adm:exempt_off"}},
		{{Text: "◀️ منو", CallbackData: "adm:help"}},
	}}
	_ = cli.SendMessageMarkup(chatID, txt, kb)
}

func (b *AdminBot) approveOrder(id uint) string {
	var order models.ShopOrder
	if b.DB.First(&order, id).Error != nil {
		return "سفارش پیدا نشد"
	}
	if order.Status == "approved" {
		return "سفارش قبلاً تأیید شده (#" + fmt.Sprint(id) + ")"
	}
	if order.Kind == "wallet_topup" {
		if err := shop.FulfillWalletTopup(b.DB, &order); err != nil {
			return "خطا در شارژ کیف پول: " + err.Error()
		}
		ut := GetSetting(b.DB, KeyUserToken)
		if ut != "" && order.TelegramChatID != 0 {
			w, _ := shop.GetOrCreateWallet(b.DB, order.TelegramUserID)
			_ = NewClient(ut).SendMessage(order.TelegramChatID, fmt.Sprintf("✅ شارژ کیف پول تأیید شد.\n+%s تومان\nموجودی: <b>%s</b> تومان", formatToman(order.PriceToman), formatToman(w.BalanceToman)))
		}
		AuditTelegram(b.DB, 0, "finance", "tg.order.approve", fmt.Sprint(order.ID), map[string]any{"kind": "wallet_topup", "amount": order.PriceToman})
		return fmt.Sprintf("✅ شارژ کیف پول سفارش #%d · %s تومان", order.ID, formatToman(order.PriceToman))
	}
	if order.Kind == "gift" {
		var plan models.ShopPlan
		if b.DB.First(&plan, order.PlanID).Error != nil {
			return "پلن هدیه پیدا نشد"
		}
		// Claim the order atomically first; a double click / second admin would
		// otherwise issue two gift cards for one payment.
		claim := b.DB.Model(&models.ShopOrder{}).
			Where("id = ? AND status IN ?", order.ID, []string{"pending_payment", "pending_review"}).
			Update("status", "processing")
		if claim.Error != nil || claim.RowsAffected == 0 {
			return "این سفارش در حال پردازش است یا قبلاً بررسی شده (#" + fmt.Sprint(id) + ")"
		}
		g, err := shop.IssueGiftFromPlan(b.DB, plan, order.TelegramUserID, order.PriceToman, false, "order:"+fmt.Sprint(order.ID), 90)
		if err != nil {
			_ = b.DB.Model(&models.ShopOrder{}).Where("id = ?", order.ID).Update("status", "pending_review").Error
			return "خطا در صدور کارت هدیه: " + err.Error()
		}
		now := time.Now()
		b.DB.Model(&order).Updates(map[string]any{"status": "approved", "reviewed_at": now, "promo_code": g.Code})
		ut := GetSetting(b.DB, KeyUserToken)
		if ut != "" && order.TelegramChatID != 0 {
			_ = NewClient(ut).SendMessageMarkup(order.TelegramChatID, fmt.Sprintf("🎁 <b>کارت هدیه شما آماده است</b>\nکد:\n<code>%s</code>\nپلن: %s\nاین کد را به دوست‌تان بدهید یا با /redeem فعال کنید.", g.Code, g.PlanName), &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "🎁 کارت‌های من", CallbackData: "gift_shop"}}}})
		}
		AuditTelegram(b.DB, 0, "finance", "tg.order.approve", fmt.Sprint(order.ID), map[string]any{"kind": "gift", "code": g.Code})
		return fmt.Sprintf("✅ هدیه #%d → کد %s", order.ID, g.Code)
	}
	cl, _, err := shop.FulfillOrder(b.DB, &order)
	if err != nil {
		return "خطا در فعال‌سازی: " + err.Error()
	}
	ut := GetSetting(b.DB, KeyUserToken)
	if ut != "" && order.TelegramChatID != 0 {
		ucli := NewClient(ut)
		subURL := "/sub/" + cl.SubID
		if b.Cfg != nil && b.Cfg.SubscriptionBaseURL != "" {
			subURL = strings.TrimRight(b.Cfg.SubscriptionBaseURL, "/") + "/sub/" + cl.SubID
		}
		_ = ucli.SendMessageMarkup(order.TelegramChatID, fmt.Sprintf("✅ سفارش #%d تأیید شد.\nاشتراک فعال است.\nSub ID: <code>%s</code>\nلینک ساب: <code>%s</code>", order.ID, cl.SubID, subURL), &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "📊 وضعیت اشتراک من", CallbackData: "status"}},
			{{Text: "🛒 تمدید", CallbackData: "shop"}},
		}})
	}
	AuditTelegram(b.DB, 0, "finance", "tg.order.approve", fmt.Sprint(order.ID), map[string]any{"clientId": cl.ID, "email": cl.Email})
	NotifyReferrerForOrder(b.DB, &order)
	if b.AfterClientChange != nil {
		b.AfterClientChange()
	}
	return fmt.Sprintf("✅ تأیید شد #%d → کلاینت #%d (%s)", order.ID, cl.ID, cl.Email)
}

func (b *AdminBot) rejectOrder(id uint) string {
	var order models.ShopOrder
	if b.DB.First(&order, id).Error != nil {
		return "سفارش پیدا نشد"
	}
	now := time.Now()
	res := b.DB.Model(&models.ShopOrder{}).
		Where("id = ? AND status IN ?", id, []string{"pending_payment", "pending_review"}).
		Updates(map[string]any{"status": "rejected", "reviewed_at": now})
	if res.RowsAffected == 0 {
		return "سفارش قابل رد نیست (وضعیت: " + order.Status + ")"
	}
	ut := GetSetting(b.DB, KeyUserToken)
	if ut != "" && order.TelegramChatID != 0 {
		_ = NewClient(ut).SendMessage(order.TelegramChatID, fmt.Sprintf("❌ سفارش #%d رد شد.", id))
	}
	AuditTelegram(b.DB, 0, "finance", "tg.order.reject", fmt.Sprint(id), nil)
	return fmt.Sprintf("❌ رد شد #%d", id)
}

func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	i := -1
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}
