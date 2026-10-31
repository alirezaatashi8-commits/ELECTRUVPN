package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/devices"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserBot: status, shop, payment receipt, renewal UX.
type UserBot struct {
	DB                *gorm.DB
	AfterClientChange func() // optional: regenerate xray after trial/purchase
}

func (b *UserBot) Run(ctx context.Context) {
	var cli *Client
	var lastToken string
	cmdsReady := false
	for {
		if ctx.Err() != nil {
			return
		}
		token := GetSetting(b.DB, KeyUserToken)
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
		// Keep the same Client so getUpdates offset is preserved across polls.
		if cli == nil || token != lastToken {
			cli = NewClient(token)
			lastToken = token
			cmdsReady = false
			_ = cli.DeleteWebhook()
		}
		if !cmdsReady {
			if err := cli.SetMyCommands([]BotCommand{
				{Command: "start", Description: "منوی اصلی"},
				{Command: "status", Description: "وضعیت اشتراک"},
				{Command: "shop", Description: "خرید / تمدید"},
				{Command: "customgb", Description: "حجم دلخواه"},
				{Command: "wallet", Description: "کیف پول"},
				{Command: "configs", Description: "کانفیگ‌های من"},
				{Command: "trial", Description: "تست رایگان"},
				{Command: "support", Description: "پشتیبانی"},
				{Command: "backup", Description: "پشتیبان ابری"},
				{Command: "help", Description: "راهنما"},
			}); err == nil {
				cmdsReady = true
			}
		}
		if err := b.pollOnce(ctx, cli); err != nil {
			slog.Warn("user telegram bot", "error", err)
			// On conflict / network errors, recreate client after a pause
			if strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "409") {
				// Keep the same Client: re-creating it reset the getUpdates offset to 0
				// and re-delivered (and re-executed) updates that were already handled.
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

func (b *UserBot) pollOnce(ctx context.Context, cli *Client) error {
	updates, err := cli.GetUpdates(25)
	if err != nil {
		return err
	}
	for _, u := range updates {
		u := u
		safeHandle("user bot", func() {
			if u.CallbackQuery != nil {
				b.onCallback(cli, u.CallbackQuery)
				return
			}
			if u.Message != nil {
				b.onMessage(cli, u.Message)
			}
		})
	}
	return nil
}

// safeHandle runs one update handler and turns a panic into a log line. These
// handlers run inside the panel process; one malformed update (nil message,
// unexpected data) used to take the whole panel down, and because Telegram
// re-delivers it after the restart the crash repeated.
func safeHandle(who string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("telegram handler panic recovered", "bot", who, "panic", fmt.Sprint(r))
		}
	}()
	fn()
}

func (b *UserBot) mainMenu() *InlineKeyboardMarkup {
	rows := [][]InlineKeyboardButton{
		{{Text: "📊 وضعیت اشتراک من", CallbackData: "status"}},
		{{Text: "🛒 خرید / تمدید", CallbackData: "shop"}, {Text: "🎁 تست رایگان", CallbackData: "trial"}},
		{{Text: "📦 حجم دلخواه", CallbackData: "custom_gb"}, {Text: "🏷 کد تخفیف", CallbackData: "promo_hint"}},
		{{Text: "💰 کیف پول", CallbackData: "wallet"}, {Text: "🧾 تراکنش‌ها", CallbackData: "txns"}},
		{{Text: "📦 کانفیگ‌های من", CallbackData: "myconfigs"}, {Text: "📁 فایل + QR", CallbackData: "config"}},
		{{Text: "📱 دستگاه‌ها", CallbackData: "devices"}, {Text: "🔄 تمدید", CallbackData: "shop"}},
		{{Text: "🎫 پشتیبانی", CallbackData: "support"}, {Text: "📋 تیکت‌های من", CallbackData: "mytickets"}},
		{{Text: "☁️ پشتیبان ابری", CallbackData: "cloud_backup"}, {Text: "♻️ بازیابی", CallbackData: "cloud_restore"}},
		{{Text: "🔗 دعوت دوستان", CallbackData: "referral"}, {Text: "🎁 کارت هدیه", CallbackData: "gift_shop"}},
		{{Text: "🖥 پورتال وب", CallbackData: "portal_web"}, {Text: "📲 اپ پیشنهادی", CallbackData: "apps"}},
		{{Text: "🆘 راهنما", CallbackData: "help"}},
	}
	base := strings.TrimRight(GetSetting(b.DB, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(GetSetting(b.DB, "public_host"), "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	if base != "" {
		rows = append([][]InlineKeyboardButton{{{Text: "🖥 داشبورد گرافیکی", WebApp: &WebAppInfo{URL: base + "/tma/"}}}}, rows...)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// replyMainKeyboard is the permanent keyboard next to the Telegram message input.
func (b *UserBot) replyMainKeyboard() *ReplyKeyboardMarkup {
	row1 := []ReplyKeyboardButton{{Text: "🏠 منوی اصلی"}}
	base := strings.TrimRight(GetSetting(b.DB, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(GetSetting(b.DB, "public_host"), "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	if base != "" {
		row1 = append(row1, ReplyKeyboardButton{Text: "🖥 داشبورد", WebApp: &WebAppInfo{URL: base + "/tma/"}})
	}
	return &ReplyKeyboardMarkup{
		Keyboard: [][]ReplyKeyboardButton{
			row1,
			{{Text: "📊 وضعیت"}, {Text: "🛒 خرید"}, {Text: "🎁 تست رایگان"}},
			{{Text: "📦 حجم دلخواه"}, {Text: "💰 کیف پول"}, {Text: "🧾 تراکنش‌ها"}},
			{{Text: "📦 کانفیگ‌های من"}, {Text: "📱 دستگاه‌ها"}},
			{{Text: "🎫 پشتیبانی"}, {Text: "📋 تیکت‌های من"}},
			{{Text: "☁️ پشتیبان ابری"}, {Text: "♻️ بازیابی"}, {Text: "🎁 کارت هدیه"}},
			{{Text: "🔗 دعوت دوستان"}, {Text: "🏷 کد تخفیف"}, {Text: "🖥 پورتال وب"}},
			{{Text: "📲 اپ پیشنهادی"}, {Text: "🆘 راهنما"}},
		},
		IsPersistent:          true,
		ResizeKeyboard:        true,
		InputFieldPlaceholder: "از دکمه‌های پایین انتخاب کنید…",
	}
}

func (b *UserBot) ensureReplyKeyboard(cli *Client, chatID int64) {
	// Lightweight ping so Telegram attaches/updates the persistent keyboard.
	_ = cli.SendMessageReplyKB(chatID, "⬇️ منوی پایین صفحه همیشه در دسترس است.", b.replyMainKeyboard())
}

func (b *UserBot) onMessage(cli *Client, m *Message) {
	chatID := m.Chat.ID
	fromID := chatID
	name := ""
	if m.From != nil {
		fromID = m.From.ID
		name = m.From.FirstName
		if m.From.Username != "" {
			name += " @" + m.From.Username
		}
	}
	// Allow only membership check text during locks — gate everything else
	rawEarly := strings.TrimSpace(m.Text)
	if rawEarly != "✅ عضو شدم — بررسی عضویت" && rawEarly != "/check_join" {
		if blocked, msg, kb := UserGate(b.DB, cli, fromID); blocked {
			if kb != nil {
				_ = cli.SendMessageMarkup(chatID, msg, kb)
			} else {
				_ = cli.SendMessage(chatID, msg)
			}
			return
		}
	}
	// Photo / document
	if len(m.Photo) > 0 || (m.Document != nil && m.Document.FileID != "") {
		fileID := ""
		kind := "document"
		if len(m.Photo) > 0 {
			fileID = m.Photo[len(m.Photo)-1].FileID
			kind = "photo"
		} else {
			fileID = m.Document.FileID
		}
		// composing support ticket with screenshot
		if IsUserComposingTicket(fromID) {
			body := strings.TrimSpace(m.Caption)
			if body == "" {
				body = "[اسکرین‌شات / فایل ضمیمه]"
			}
			if replyTo, ok := TakeUserReplyTicket(fromID); ok {
				_ = AppendUserTicketMessage(b.DB, replyTo, fromID, body, fileID, kind)
				_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("📎 به تیکت #%d اضافه شد.", replyTo), b.mainMenu())
				return
			}
			SetUserComposingTicket(fromID, false)
			tkt, err := CreateUserTicket(b.DB, fromID, chatID, "پشتیبانی + اسکرین", body, fileID, kind)
			if err != nil {
				_ = cli.SendMessageMarkup(chatID, "خطا در ثبت تیکت.", b.mainMenu())
				return
			}
			_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ تیکت #%d با تصویر ثبت شد.\nپشتیبانی از همین چت پاسخ می‌دهد.", tkt.ID), b.mainMenu())
			return
		}
		// open ticket follow-up photo
		var open models.Ticket
		if b.DB.Where("telegram_user_id = ? AND status <> ?", fromID, "closed").Order("id desc").First(&open).Error == nil {
			body := strings.TrimSpace(m.Caption)
			if body == "" {
				body = "[اسکرین‌شات]"
			}
			_ = AppendUserTicketMessage(b.DB, open.ID, fromID, body, fileID, kind)
			_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("📎 تصویر به تیکت #%d اضافه و برای ادمین ارسال شد.", open.ID), b.mainMenu())
			return
		}
		// else: payment receipt
		b.handleReceipt(cli, fromID, chatID, fileID, m.Caption)
		return
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}
	parts := strings.Fields(text)
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	if i := strings.Index(cmd, "@"); i > 0 {
		cmd = cmd[:i]
	}
	// User entering custom GB amount
	if IsPendingGiftRedeem(fromID) {
		text = strings.TrimSpace(text)
		if text == "/cancel" || text == "لغو" {
			SetPendingGiftRedeem(fromID, false)
			_ = cli.SendMessageMarkup(chatID, "فعال‌سازی هدیه لغو شد.", b.mainMenu())
			return
		}
		b.redeemGiftCode(cli, fromID, chatID, text)
		return
	}
	if IsPendingRestore(fromID) {
		text = strings.TrimSpace(text)
		if text == "/cancel" || text == "لغو" || text == "❌ لغو" {
			SetPendingRestore(fromID, false)
			_ = cli.SendMessageMarkup(chatID, "بازیابی لغو شد.", b.mainMenu())
			return
		}
		snap, err := RestoreCloudBackup(b.DB, text, fromID, chatID)
		if err != nil {
			// keep pending so user can retry the code without re-opening the menu
			_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error()+"\nکد را دوباره بفرستید یا /cancel بزنید.", b.mainMenu())
			return
		}
		SetPendingRestore(fromID, false)
		msg := "✅ <b>بازیابی موفق</b>\nاکانت‌ها به این تلگرام لینک شدند.\n\n" + formatCloudSnapshotSummary(snap)
		msg += "\nاز «وضعیت اشتراک» و «کانفیگ‌های من» استفاده کنید."
		_ = cli.SendMessageMarkup(chatID, msg, b.mainMenu())
		return
	}
	if IsPendingCustomGB(fromID) {
		raw := strings.TrimSpace(m.Text)
		if raw == "/cancel" || raw == "انصراف" {
			SetPendingCustomGB(fromID, false)
			_ = cli.SendMessageMarkup(chatID, "خرید حجم دلخواه لغو شد.", b.mainMenu())
			return
		}
		gb, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(raw, "گیگ", "")))
		if err != nil || gb <= 0 {
			_ = cli.SendMessage(chatID, "یک عدد صحیح گیگ وارد کنید (مثال: 50)\n/cancel برای انصراف")
			return
		}
		SetPendingCustomGB(fromID, false)
		b.quoteCustomGB(cli, fromID, chatID, name, gb)
		return
	}

	// User is composing a support ticket
	if IsUserComposingTicket(fromID) {
		body := strings.TrimSpace(m.Text)
		if body == "/cancel" || body == "انصراف" {
			SetUserComposingTicket(fromID, false)
			_ = cli.SendMessageMarkup(chatID, "ثبت تیکت لغو شد.", b.mainMenu())
			return
		}
		if replyTo, ok := TakeUserReplyTicket(fromID); ok {
			if err := AppendUserTicketMessage(b.DB, replyTo, fromID, body, "", ""); err != nil {
				_ = cli.SendMessageMarkup(chatID, "خطا: "+err.Error(), b.mainMenu())
				return
			}
			_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ پاسخ به تیکت #%d ارسال شد.", replyTo), b.mainMenu())
			return
		}
		SetUserComposingTicket(fromID, false)
		tkt, err := CreateUserTicket(b.DB, fromID, chatID, "پشتیبانی ربات", body, "", "")
		if err != nil {
			_ = cli.SendMessageMarkup(chatID, "خطا در ثبت تیکت.", b.mainMenu())
			return
		}
		_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ تیکت #%d ثبت شد.\nپاسخ ادمین همین‌جا نمایش داده می‌شود.\nمی‌توانید اسکرین‌شات هم بفرستید.", tkt.ID), b.mainMenu())
		return
	}

	// Persistent reply-keyboard labels (next to message input)
	rawText := strings.TrimSpace(m.Text)
	switch rawText {
	case "🏠 منوی اصلی", "منوی اصلی", "منو":
		{
			w, _ := shop.GetOrCreateWallet(b.DB, fromID)
			_ = cli.SendMessageFull(chatID, fmt.Sprintf("🏠 <b>منوی اصلی</b>\n💰 موجودی کیف پول: <b>%s</b> تومان\nاز دکمه‌های زیر یا منوی پایین صفحه استفاده کنید:", formatToman(w.BalanceToman)), b.mainMenu(), b.replyMainKeyboard())
		}
		return
	case "📊 وضعیت", "وضعیت":
		_ = cli.SendMessageMarkup(chatID, b.statusForTelegram(fromID), b.statusActions())
		return
	case "🛒 خرید", "خرید":
		b.sendShop(cli, chatID, fromID)
		return
	case "📦 حجم دلخواه", "حجم دلخواه":
		b.startCustomGB(cli, fromID, chatID)
		return
	case "💰 کیف پول", "کیف پول":
		b.sendWallet(cli, fromID, chatID)
		return
	case "📱 دستگاه‌ها", "دستگاه‌ها":
		b.sendDevices(cli, fromID, chatID)
		return
	case "☁️ پشتیبان ابری", "پشتیبان ابری", "پشتیبان", "بکاپ", "backup":
		b.handleCloudBackup(cli, fromID, chatID)
		return
	case "♻️ بازیابی", "بازیابی", "بازیابی اکانت", "restore":
		b.startCloudRestore(cli, fromID, chatID)
		return
	case "📦 کانفیگ‌های من", "کانفیگ‌های من", "کانفیگها", "کانفیگ های من":
		b.sendMyConfigs(cli, fromID, chatID)
		return
	case "🎫 پشتیبانی", "پشتیبانی", "تیکت":
		b.startSupport(cli, fromID, chatID)
		return
	case "📋 تیکت‌های من", "تیکت‌های من", "تیکتهای من":
		b.listMyTickets(cli, fromID, chatID)
		return
	case "📲 اپ پیشنهادی", "اپ پیشنهادی", "نپستر", "napsternet":
		_ = cli.SendMessageMarkup(chatID, recommendedAppsMessage(), recommendedAppsKeyboard())
		return
	case "✅ عضو شدم — بررسی عضویت", "عضو شدم":
		b.checkChannelJoin(cli, fromID, chatID)
		return
	case "🔗 دعوت دوستان", "دعوت دوستان", "دعوت", "referral", "معرف":
		b.sendReferral(cli, fromID, chatID)
		return
	case "🎁 کارت هدیه", "کارت هدیه", "هدیه":
		b.sendGiftShop(cli, chatID, fromID)
		return
	case "🖥 پورتال وب", "پورتال وب", "پورتال":
		b.sendPortalLink(cli, fromID, chatID)
		return
	case "🎁 تست رایگان", "تست رایگان", "تست":
		b.giveTrial(cli, fromID, chatID)
		return
	case "🧾 تراکنش‌ها", "تراکنش‌ها", "تراکنشها":
		b.sendTransactions(cli, fromID, chatID)
		return
	case "🏷 کد تخفیف", "کد تخفیف", "تخفیف":
		_ = cli.SendMessageMarkup(chatID, "🏷 کد تخفیف را این‌طور بفرستید:\n<code>/promo YALDA20</code>\nسپس از «🛒 خرید» پلن را انتخاب کنید.", b.mainMenu())
		return
	case "🆘 راهنما", "راهنما":
		_ = cli.SendMessageMarkup(chatID, userHelp(), b.mainMenu())
		return
	}

	switch cmd {
	case "start", "menu":
		b.handleStartPayload(cli, fromID, chatID, parts)
	case "status", "usage":
		if len(parts) >= 2 {
			_ = cli.SendMessageMarkup(chatID, b.lookupText(strings.Join(parts[1:], " ")), b.mainMenu())
		} else {
			_ = cli.SendMessageMarkup(chatID, b.statusForTelegram(fromID), b.statusActions())
		}
	case "shop", "buy":
		b.sendShop(cli, chatID, fromID)
	case "wallet", "balance":
		b.sendWallet(cli, fromID, chatID)
	case "txns", "transactions":
		b.sendTransactions(cli, fromID, chatID)
	case "devices", "device":
		b.sendDevices(cli, fromID, chatID)
	case "customgb", "custom_gb", "volume":
		b.startCustomGB(cli, fromID, chatID)
	case "cloud_backup", "backup", "cloudbackup":
		b.handleCloudBackup(cli, fromID, chatID)
	case "cloud_restore", "restore":
		b.startCloudRestore(cli, fromID, chatID)
	case "cloud_revoke", "revoke_backup":
		b.handleCloudRevoke(cli, fromID, chatID)
	case "myconfigs", "configs", "myconfig":
		b.sendMyConfigs(cli, fromID, chatID)
	case "support", "ticket", "tickets":
		b.startSupport(cli, fromID, chatID)
	case "apps", "app", "napsternet", "download":
		_ = cli.SendMessageMarkup(chatID, recommendedAppsMessage(), recommendedAppsKeyboard())
	case "help":
		_ = cli.SendMessageMarkup(chatID, userHelp(), b.mainMenu())
	case "referral", "invite", "ref":
		b.sendReferral(cli, fromID, chatID)
	case "trial", "freetrial":
		b.giveTrial(cli, fromID, chatID)
	case "web", "portal", "panel":
		b.sendPortalLink(cli, fromID, chatID)
	case "gift", "gifts", "giftcard":
		b.sendGiftShop(cli, chatID, fromID)
	case "redeem", "redeengift":
		if len(parts) >= 2 {
			b.redeemGiftCode(cli, fromID, chatID, parts[1])
		} else {
			b.startGiftRedeem(cli, fromID, chatID)
		}
	case "promo", "discount", "code":
		if len(parts) < 2 {
			_ = cli.SendMessageMarkup(chatID, "کد تخفیف را وارد کنید:\n<code>/promo YALDA20</code>\nسپس از فروشگاه خرید کنید.", b.mainMenu())
			return
		}
		code := parts[1]
		// Validate existence with a high base so min-order doesn't false-fail on lock
		q, err := shop.QuotePromo(b.DB, code, fromID, 1_000_000_000)
		if err != nil {
			_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
			return
		}
		SetPendingPromo(fromID, q.Code)
		_ = cli.SendMessageMarkup(chatID, "✅ کد <b>"+q.Code+"</b> برای خرید بعدی شما ذخیره شد.\n"+q.Message+"\nاز «🛒 خرید» پلن را انتخاب کنید.", b.mainMenu())
	default:
		if !strings.HasPrefix(text, "/") {
			_ = cli.SendMessageMarkup(chatID, b.lookupText(text), b.mainMenu())
			return
		}
		_ = cli.SendMessageMarkup(chatID, userHelp(), b.mainMenu())
	}
	_ = name
}

func (b *UserBot) onCallback(cli *Client, cq *CallbackQuery) {
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
	data := strings.TrimSpace(cq.Data)
	_ = cli.AnswerCallback(cq.ID, "")
	if data != "check_join" {
		if blocked, msg, kb := UserGate(b.DB, cli, fromID); blocked {
			if kb != nil {
				_ = cli.SendMessageMarkup(chatID, msg, kb)
			} else {
				_ = cli.SendMessage(chatID, msg)
			}
			return
		}
	}
	switch {
	case data == "status":
		_ = cli.SendMessageMarkup(chatID, b.statusForTelegram(fromID), b.statusActions())
	case data == "shop":
		SetPendingLoyalty(fromID, false)
		b.sendShop(cli, chatID, fromID)
	case data == "shop_loyalty":
		SetPendingLoyalty(fromID, true)
		b.sendShop(cli, chatID, fromID)
	case data == "gift_shop":
		b.sendGiftShop(cli, chatID, fromID)
	case data == "gift_redeem":
		b.startGiftRedeem(cli, fromID, chatID)
	case data == "custom_gb":
		b.startCustomGB(cli, fromID, chatID)
	case strings.HasPrefix(data, "cgb_card:"):
		gb, _ := strconv.Atoi(strings.TrimPrefix(data, "cgb_card:"))
		nm := ""
		if cq.From != nil {
			nm = cq.From.FirstName
			if cq.From.Username != "" {
				nm += " @" + cq.From.Username
			}
		}
		b.orderCustomGB(cli, fromID, chatID, nm, gb, false)
	case strings.HasPrefix(data, "cgb_wallet:"):
		gb, _ := strconv.Atoi(strings.TrimPrefix(data, "cgb_wallet:"))
		nm := ""
		if cq.From != nil {
			nm = cq.From.FirstName
			if cq.From.Username != "" {
				nm += " @" + cq.From.Username
			}
		}
		b.orderCustomGB(cli, fromID, chatID, nm, gb, true)
	case data == "trial":
		b.giveTrial(cli, fromID, chatID)
	case data == "referral":
		b.sendReferral(cli, fromID, chatID)
	case data == "config":
		b.sendConfigFiles(cli, fromID, chatID)
	case data == "cloud_backup":
		b.handleCloudBackup(cli, fromID, chatID)
	case data == "cloud_restore":
		b.startCloudRestore(cli, fromID, chatID)
	case data == "cloud_backup_rotate":
		b.handleCloudBackup(cli, fromID, chatID)
	case data == "cloud_revoke":
		b.handleCloudRevoke(cli, fromID, chatID)
	case data == "myconfigs":
		b.sendMyConfigs(cli, fromID, chatID)
	case strings.HasPrefix(data, "refreshcfg:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "refreshcfg:"), 10, 64)
		b.refreshConfig(cli, fromID, chatID, uint(id))
	case strings.HasPrefix(data, "cfgfile:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "cfgfile:"), 10, 64)
		b.sendConfigFileForClient(cli, fromID, chatID, uint(id))
	case data == "support":
		b.startSupport(cli, fromID, chatID)
	case data == "apps":
		_ = cli.SendMessageMarkup(chatID, recommendedAppsMessage(), recommendedAppsKeyboard())
	case data == "check_join":
		b.checkChannelJoin(cli, fromID, chatID)
	case data == "mytickets":
		b.listMyTickets(cli, fromID, chatID)
	case strings.HasPrefix(data, "ticket_reply:"):
		tid, _ := strconv.ParseUint(strings.TrimPrefix(data, "ticket_reply:"), 10, 64)
		SetUserReplyToTicket(fromID, uint(tid))
		_ = cli.SendMessage(chatID, fmt.Sprintf("↩️ پاسخ شما به تیکت #%d ارسال می‌شود. متن یا اسکرین‌شات بفرستید.\n/cancel برای انصراف", tid))
	case data == "wallet":
		b.sendWallet(cli, fromID, chatID)
	case data == "txns":
		b.sendTransactions(cli, fromID, chatID)
	case data == "devices":
		b.sendDevices(cli, fromID, chatID)
	case data == "revoke_all":
		b.requestSensitiveConfirm(cli, fromID, chatID, shop.SensKindRevokeAll)
	case data == "reset_uuid":
		b.requestSensitiveConfirm(cli, fromID, chatID, shop.SensKindResetUUID)
	case strings.HasPrefix(data, "sens_ok:"):
		b.confirmSensitive(cli, fromID, chatID, strings.TrimPrefix(data, "sens_ok:"))
	case strings.HasPrefix(data, "sens_no:"):
		tok := strings.TrimPrefix(data, "sens_no:")
		shop.ConsumeSensitivePending(b.DB, tok)
		_ = cli.SendMessageMarkup(chatID, "❌ عملیات لغو شد.", b.mainMenu())
	case strings.HasPrefix(data, "revdev:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "revdev:"), 10, 64)
		b.revokeDevice(cli, fromID, chatID, uint(id))
	case strings.HasPrefix(data, "wtop:"):
		amt, _ := strconv.ParseInt(strings.TrimPrefix(data, "wtop:"), 10, 64)
		b.startWalletTopup(cli, fromID, chatID, cq.From, amt)
	case data == "autorenew_on":
		b.setAutoRenew(cli, fromID, chatID, true)
	case data == "autorenew_off":
		b.setAutoRenew(cli, fromID, chatID, false)
	case strings.HasPrefix(data, "arplan:"):
		pid, _ := strconv.ParseUint(strings.TrimPrefix(data, "arplan:"), 10, 64)
		b.setAutoRenewPlan(cli, fromID, chatID, uint(pid))
	case data == "help":
		_ = cli.SendMessageMarkup(chatID, userHelp(), b.mainMenu())
	case data == "portal_web":
		b.sendPortalLink(cli, fromID, chatID)
	case data == "promo_hint":
		_ = cli.SendMessageMarkup(chatID, "🏷 کد تخفیف:\n<code>/promo CODE</code>\nبعد از اعمال، از فروشگاه خرید کنید.", b.mainMenu())
	case strings.HasPrefix(data, "survey:"):
		score := strings.TrimPrefix(data, "survey:")
		_ = cli.SendMessageMarkup(chatID, "🙏 از بازخورد شما متشکریم (امتیاز: "+score+").", b.mainMenu())
		// store lightweight
		_ = b.DB.Create(&models.PanelSetting{Key: fmt.Sprintf("survey_%d_%d", fromID, time.Now().Unix()), Value: score}).Error
	case data == "menu":
		_ = cli.SendMessageFull(chatID, "🏠 منوی اصلی:", b.mainMenu(), b.replyMainKeyboard())
	case strings.HasPrefix(data, "cancelorder:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "cancelorder:"), 10, 64)
		b.cancelOrder(cli, fromID, chatID, uint(id))
	case strings.HasPrefix(data, "paywallet:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "paywallet:"), 10, 64)
		b.payOrderFromWallet(cli, fromID, chatID, uint(id))
	case strings.HasPrefix(data, "plan:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "plan:"), 10, 64)
		b.startOrder(cli, fromID, chatID, cq.From, uint(id))
	case strings.HasPrefix(data, "giftplan:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(data, "giftplan:"), 10, 64)
		b.startGiftPurchase(cli, fromID, chatID, cq.From, uint(id))
	case strings.HasPrefix(data, "renew:"):
		b.sendShop(cli, chatID, fromID)
	default:
		_ = cli.SendMessageMarkup(chatID, "دکمه ناشناخته — از منوی اصلی استفاده کنید.", b.mainMenu())
	}
}

func userHelp() string {
	return `📖 <b>راهنما</b>

📊 وضعیت اشتراک — حجم و روز باقی‌مانده
🛒 خرید / تمدید — پلن و کارت‌به‌کارت
📦 حجم دلخواه — خرید گیگ سفارشی
💰 کیف پول — موجودی و شارژ
📱 دستگاه‌ها — مدیریت دستگاه‌های متصل
📦 کانفیگ‌های من — لینک و QR
🎁 تست رایگان — اکانت آزمایشی (یک‌بار)
☁️ پشتیبان ابری / ♻️ بازیابی
🎫 پشتیبانی — تیکت (+ عکس)
🔗 دعوت دوستان — پاداش معرف
🎁 کارت هدیه — خرید یا فعال‌سازی کد
🖥 پورتال وب / داشبورد مینی‌اپ
📲 اپ پیشنهادی

دستورات:
/status /shop /customgb /trial /configs /wallet /support /backup /restore /referral /gift /menu`
}

func (b *UserBot) statusForTelegram(tgUserID int64) string {
	w, _ := shop.GetOrCreateWallet(b.DB, tgUserID)
	balLine := fmt.Sprintf("\n\n💰 موجودی کیف پول: <b>%s</b> تومان", formatToman(w.BalanceToman))
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", tgUserID).First(&link).Error != nil {
		return "هنوز اشتراکی به حساب تلگرام شما وصل نیست.\nاز «🛒 خرید / تمدید» پلن بخرید یا Sub ID خود را بفرستید." + balLine
	}
	var cl models.Client
	if b.DB.First(&cl, link.ClientID).Error != nil {
		return "کلاینت پیدا نشد. دوباره خرید کنید یا با پشتیبانی تماس بگیرید." + balLine
	}
	var ib models.Inbound
	_ = b.DB.First(&ib, cl.InboundID)
	hs := shop.ComputeHealthScore(cl, shop.HealthInput{})
	healthLine := fmt.Sprintf("\n🩺 سلامت اشتراک: <b>%d/100</b> (%s) — %s", hs.Score, hs.Label, hs.Summary)
	return shop.FormatStatusCard(cl, ib.Remark) + healthLine + balLine
}

// statusActions returns quick action buttons under the status card.
func (b *UserBot) statusActions() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🛒 تمدید / خرید", CallbackData: "shop"}, {Text: "📦 کانفیگ‌ها", CallbackData: "myconfigs"}},
		{{Text: "💰 کیف پول", CallbackData: "wallet"}, {Text: "📱 دستگاه‌ها", CallbackData: "devices"}},
		{{Text: "🎫 پشتیبانی", CallbackData: "support"}, {Text: "🏠 منو", CallbackData: "menu"}},
	}}
}

func (b *UserBot) lookupText(key string) string {
	key = strings.TrimSpace(key)
	var cl models.Client
	err := b.DB.Where("sub_id = ?", key).First(&cl).Error
	if err != nil {
		err = b.DB.Where("LOWER(email) = LOWER(?)", key).First(&cl).Error
	}
	if err != nil {
		return "❌ حساب پیدا نشد."
	}
	var ib models.Inbound
	_ = b.DB.First(&ib, cl.InboundID)
	return shop.FormatStatusCard(cl, ib.Remark)
}

func (b *UserBot) sendShop(cli *Client, chatID, fromID int64) {
	_, _, _, enabled := shop.PaymentInfo(b.DB)
	if !enabled {
		_ = cli.SendMessageMarkup(chatID, "🛒 فروشگاه فعلاً غیرفعال است.\nاز پنل → تلگرام → فروشگاه، گزینه «فروشگاه فعال» را روشن کنید.", b.mainMenu())
		return
	}
	var plans []models.ShopPlan
	b.DB.Where("enabled = ?", true).Order("sort_order asc, id asc").Find(&plans)
	if len(plans) == 0 {
		_ = cli.SendMessageMarkup(chatID, "هنوز پلنی تعریف نشده.\nاز پنل → تلگرام → فروشگاه، پلن اضافه کنید.", b.mainMenu())
		return
	}
	var rows [][]InlineKeyboardButton
	var sb strings.Builder
	sb.WriteString("🛒 <b>پلن‌های موجود</b>\n\n")
	wbActive, wbPct, wbCfg := shop.HasWinbackOffer(b.DB, fromID)
	if wbActive {
		sb.WriteString(fmt.Sprintf("🎁 <b>تخفیف بازگشت %d%% فعال است</b>\n\n", wbPct))
	} else if GetPendingLoyalty(fromID) {
		if lp := shop.LoyaltyDiscountPct(b.DB); lp > 0 {
			sb.WriteString(fmt.Sprintf("🎁 <b>تخفیف وفاداری %d%%</b> برای تمدید سریع فعال است\n\n", lp))
		}
	}
	for _, p := range plans {
		gb := "نامحدود"
		if p.TotalGB > 0 {
			gb = fmt.Sprintf("%d GB", p.TotalGB)
		}
		price := p.PriceToman
		priceLabel := formatToman(price) + " تومان"
		if wbActive && shop.PlanEligibleForWinback(wbCfg, p.ID) {
			price = shop.DiscountedPrice(p.PriceToman, wbPct)
			priceLabel = fmt.Sprintf("<s>%s</s> → <b>%s</b> تومان", formatToman(p.PriceToman), formatToman(price))
		} else if GetPendingLoyalty(fromID) {
			if lp := shop.LoyaltyDiscountPct(b.DB); lp > 0 {
				price = shop.DiscountedPrice(p.PriceToman, lp)
				priceLabel = fmt.Sprintf("<s>%s</s> → <b>%s</b> تومان", formatToman(p.PriceToman), formatToman(price))
			}
		}
		sb.WriteString(fmt.Sprintf("• <b>%s</b>\n  %s · %d روز · %s\n", p.Name, gb, p.DurationDays, priceLabel))
		if p.Description != "" {
			sb.WriteString("  " + p.Description + "\n")
		}
		sb.WriteString("\n")
		btn := fmt.Sprintf("%s — %s ت", p.Name, formatToman(price))
		if len([]rune(btn)) > 60 {
			btn = string([]rune(btn)[:57]) + "…"
		}
		rows = append(rows, []InlineKeyboardButton{{
			Text: btn, CallbackData: fmt.Sprintf("plan:%d", p.ID),
		}})
	}
	rows = append(rows,
		[]InlineKeyboardButton{{Text: "📦 حجم دلخواه", CallbackData: "custom_gb"}, {Text: "🎁 کارت هدیه", CallbackData: "gift_shop"}},
		[]InlineKeyboardButton{{Text: "💰 کیف پول", CallbackData: "wallet"}, {Text: "🏷 کد تخفیف", CallbackData: "promo_hint"}},
		[]InlineKeyboardButton{{Text: "◀️ منو", CallbackData: "menu"}},
	)
	_ = cli.SendMessageMarkup(chatID, sb.String(), &InlineKeyboardMarkup{InlineKeyboard: rows})
}

func formatToman(n int64) string {
	s := fmt.Sprintf("%d", n)
	// thousand separators
	var out []byte
	for i, c := range reverse(s) {
		if i > 0 && i%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	// reverse back
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func (b *UserBot) startOrder(cli *Client, fromID, chatID int64, from *User, planID uint) {
	var plan models.ShopPlan
	if b.DB.First(&plan, planID).Error != nil || !plan.Enabled {
		_ = cli.SendMessage(chatID, "این پلن موجود نیست.")
		return
	}
	// cancel previous pending for same user
	_ = b.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status = ?", fromID, "pending_payment").
		Update("status", "cancelled").Error

	name := ""
	if from != nil {
		name = from.FirstName
		if from.Username != "" {
			name += " @" + from.Username
		}
	}
	price := plan.PriceToman
	planLabel := plan.Name
	kind := "plan"
	var disc int64
	prorateMsg := ""
	if active, pct, wcfg := shop.HasWinbackOffer(b.DB, fromID); active && shop.PlanEligibleForWinback(wcfg, plan.ID) {
		price = shop.DiscountedPrice(plan.PriceToman, pct)
		planLabel = fmt.Sprintf("%s (تخفیف %d%%)", plan.Name, pct)
	} else if GetPendingLoyalty(fromID) {
		if lp := shop.LoyaltyDiscountPct(b.DB); lp > 0 {
			price = shop.DiscountedPrice(plan.PriceToman, lp)
			planLabel = fmt.Sprintf("%s (وفاداری %d%%)", plan.Name, lp)
		}
		SetPendingLoyalty(fromID, false)
	} else {
		pq := shop.QuoteProrate(b.DB, fromID, plan)
		if pq.HasActive && pq.CreditToman > 0 {
			price = pq.FinalToman
			disc = pq.CreditToman
			kind = "upgrade"
			planLabel = plan.Name + " (ارتقاء/تعویض)"
			prorateMsg = "\n\n🧮 " + pq.Message
			shop.StoreProrateExcess(b.DB, fromID, pq.ExcessToman)
		}
	}
	orig := plan.PriceToman
	promo := GetPendingPromo(fromID)
	if promo != "" {
		q, err := shop.QuotePromo(b.DB, promo, fromID, price)
		if err != nil {
			_ = cli.SendMessage(chatID, "⚠️ کد تخفیف اعمال نشد: "+err.Error())
			SetPendingPromo(fromID, "")
			promo = ""
		} else {
			price = q.FinalToman
			disc += q.DiscountToman
			promo = q.Code
			planLabel = planLabel + " · " + promo
			SetPendingPromo(fromID, "")
		}
	}
	order := models.ShopOrder{
		TelegramUserID: fromID, TelegramChatID: chatID, TelegramName: name,
		PlanID: plan.ID, PlanName: planLabel, TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: price,
		InboundID: plan.InboundID, Status: "pending_payment", Kind: kind,
		PromoCode: promo, OriginalPriceToman: orig, DiscountToman: disc,
	}
	if err := b.DB.Create(&order).Error; err != nil {
		_ = cli.SendMessage(chatID, "خطا در ثبت سفارش.")
		return
	}
	card, holder, instructions, _ := shop.PaymentInfo(b.DB)
	priceLine := formatToman(price)
	if disc > 0 {
		priceLine = formatToman(orig) + " → <b>" + formatToman(price) + "</b> (−" + formatToman(disc) + ")"
	}
	msg := fmt.Sprintf(`🧾 <b>سفارش #%d</b>
پلن: %s
مبلغ: %s تومان%s

💳 شماره کارت:
<code>%s</code>
%s

%s

پس از واریز، <b>عکس رسید</b> را همین‌جا ارسال کنید.`,
		order.ID, planLabel, priceLine, prorateMsg,
		card, func() string {
			if holder != "" {
				return "به نام: " + holder
			}
			return ""
		}(), instructions)
	payRows := [][]InlineKeyboardButton{}
	// Wallet instant pay when balance is enough
	if w, err := shop.GetOrCreateWallet(b.DB, fromID); err == nil && w.BalanceToman >= price && price > 0 {
		payRows = append(payRows, []InlineKeyboardButton{{Text: "⚡ پرداخت از کیف پول (آنی)", CallbackData: fmt.Sprintf("paywallet:%d", order.ID)}})
	}
	payRows = append(payRows,
		[]InlineKeyboardButton{{Text: "◀️ بازگشت به فروشگاه", CallbackData: "shop"}},
		[]InlineKeyboardButton{{Text: "❌ لغو سفارش", CallbackData: fmt.Sprintf("cancelorder:%d", order.ID)}},
	)
	_ = cli.SendMessageMarkup(chatID, msg+"\n\n📷 بعد از واریز، <b>عکس رسید</b> را بفرستید.", &InlineKeyboardMarkup{InlineKeyboard: payRows})
}

func (b *UserBot) cancelOrder(cli *Client, fromID, chatID int64, orderID uint) {
	var order models.ShopOrder
	if b.DB.First(&order, orderID).Error != nil || order.TelegramUserID != fromID {
		_ = cli.SendMessageMarkup(chatID, "سفارش پیدا نشد.", b.mainMenu())
		return
	}
	if order.Status != "pending_payment" && order.Status != "pending_review" {
		_ = cli.SendMessageMarkup(chatID, "این سفارش قابل لغو نیست (وضعیت: "+order.Status+").", b.mainMenu())
		return
	}
	_ = b.DB.Model(&order).Update("status", "cancelled").Error
	_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("❌ سفارش #%d لغو شد.", orderID), b.mainMenu())
}

func (b *UserBot) payOrderFromWallet(cli *Client, fromID, chatID int64, orderID uint) {
	var order models.ShopOrder
	if b.DB.First(&order, orderID).Error != nil || order.TelegramUserID != fromID {
		_ = cli.SendMessageMarkup(chatID, "سفارش پیدا نشد.", b.mainMenu())
		return
	}
	if order.Status != "pending_payment" {
		_ = cli.SendMessageMarkup(chatID, "این سفارش در وضعیت پرداخت نیست.", b.mainMenu())
		return
	}
	if order.Kind == "wallet_topup" {
		_ = cli.SendMessageMarkup(chatID, "شارژ کیف پول را نمی‌توان با خود کیف پول پرداخت کرد.", b.mainMenu())
		return
	}
	// Keep DB status pending_payment (allowed by fulfillOrderTx); debit + fulfill atomically.
	cl, err := shop.DebitAndFulfill(b.DB, &order, "buy", fmt.Sprintf("order:%d", order.ID), order.PlanName)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	if b.AfterClientChange != nil {
		b.AfterClientChange()
	}
	_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ پرداخت از کیف پول انجام شد.\nپلن: %s\nایمیل: <code>%s</code>\nSub ID: <code>%s</code>", order.PlanName, cl.Email, cl.SubID), b.mainMenu())
	b.sendMyConfigs(cli, fromID, chatID)
}

func (b *UserBot) handleReceipt(cli *Client, fromID, chatID int64, fileID, caption string) {
	var order models.ShopOrder
	if b.DB.Where("telegram_user_id = ? AND status = ?", fromID, "pending_payment").Order("id desc").First(&order).Error != nil {
		// also accept if pending_review re-upload
		if b.DB.Where("telegram_user_id = ? AND status = ?", fromID, "pending_review").Order("id desc").First(&order).Error != nil {
			_ = cli.SendMessage(chatID, "سفارش باز برای پرداخت ندارید. اول از فروشگاه پلن انتخاب کنید.")
			return
		}
	}
	order.ReceiptFileID = fileID
	order.ReceiptNote = strings.TrimSpace(caption)
	order.Status = "pending_review"
	// Only touch the receipt columns, and only while the order is still open: a
	// full Save could overwrite an approval the admin made a moment earlier.
	res := b.DB.Model(&models.ShopOrder{}).
		Where("id = ? AND status IN ?", order.ID, []string{"pending_payment", "pending_review"}).
		Updates(map[string]any{"receipt_file_id": order.ReceiptFileID, "receipt_note": order.ReceiptNote, "status": "pending_review"})
	if res.Error != nil || res.RowsAffected == 0 {
		_ = cli.SendMessage(chatID, "این سفارش دیگر باز نیست. اگر پرداخت کرده‌اید با پشتیبانی تماس بگیرید.")
		return
	}
	_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ رسید سفارش #%d دریافت شد.\nپس از تأیید ادمین، اشتراک فعال و لینک برای شما ارسال می‌شود.", order.ID), b.mainMenu())

	// notify admins via admin bot
	b.notifyAdminsNewOrder(order)
}

func (b *UserBot) sendWallet(cli *Client, fromID, chatID int64) {
	w, _ := shop.GetOrCreateWallet(b.DB, fromID)
	var link models.TelegramCustomer
	ar := "خاموش"
	planName := "—"
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error == nil {
		if link.AutoRenewEnabled {
			ar = "روشن"
		}
		if link.AutoRenewPlanID > 0 {
			var p models.ShopPlan
			if b.DB.First(&p, link.AutoRenewPlanID).Error == nil {
				planName = p.Name
			}
		}
	}
	msg := fmt.Sprintf("💰 <b>کیف پول</b>\n\nموجودی باقی‌مانده: <b>%s</b> تومان\nتمدید خودکار: %s\nپلن تمدید: %s\n\nبرای شارژ، مبلغ را انتخاب کنید یا رسید کارت‌به‌کارت بفرستید.\nاز موجودی می‌توانید پلن بخرید یا تمدید کنید.",
		formatToman(w.BalanceToman), ar, planName)
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "۵۰٬۰۰۰ ت", CallbackData: "wtop:50000"}, {Text: "۱۰۰٬۰۰۰ ت", CallbackData: "wtop:100000"}},
		{{Text: "۲۰۰٬۰۰۰ ت", CallbackData: "wtop:200000"}, {Text: "۵۰۰٬۰۰۰ ت", CallbackData: "wtop:500000"}},
		{{Text: "✅ تمدید خودکار روشن", CallbackData: "autorenew_on"}, {Text: "⛔ خاموش", CallbackData: "autorenew_off"}},
		{{Text: "◀️ منو", CallbackData: "menu"}},
	}}
	// plan pick buttons
	var plans []models.ShopPlan
	b.DB.Where("enabled = ?", true).Order("sort_order asc").Limit(5).Find(&plans)
	for _, p := range plans {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []InlineKeyboardButton{
			{Text: "پلن تمدید: " + p.Name, CallbackData: fmt.Sprintf("arplan:%d", p.ID)},
		})
	}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

func (b *UserBot) startWalletTopup(cli *Client, fromID, chatID int64, from *User, amount int64) {
	if amount < 10000 {
		_ = cli.SendMessage(chatID, "حداقل شارژ ۱۰٬۰۰۰ تومان")
		return
	}
	_ = b.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status = ? AND kind = ?", fromID, "pending_payment", "wallet_topup").
		Update("status", "cancelled").Error
	name := ""
	if from != nil {
		name = from.FirstName
	}
	order := models.ShopOrder{
		TelegramUserID: fromID, TelegramChatID: chatID, TelegramName: name,
		PlanName: "شارژ کیف پول", PriceToman: amount, Status: "pending_payment", Kind: "wallet_topup",
	}
	if err := b.DB.Create(&order).Error; err != nil {
		_ = cli.SendMessage(chatID, "خطا در ثبت")
		return
	}
	card, holder, instructions, _ := shop.PaymentInfo(b.DB)
	msg := fmt.Sprintf("💳 شارژ کیف پول — سفارش #%d\nمبلغ: <b>%s تومان</b>\n\nکارت:\n<code>%s</code>\n%s\n\n%s\n\nعکس رسید را ارسال کنید.",
		order.ID, formatToman(amount), card, holder, instructions)
	_ = cli.SendMessage(chatID, msg)
}

func (b *UserBot) setAutoRenew(cli *Client, fromID, chatID int64, on bool) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil {
		_ = cli.SendMessageMarkup(chatID, "اول اشتراک بخرید یا تست بگیرید تا حساب وصل شود.", b.mainMenu())
		return
	}
	link.AutoRenewEnabled = on
	_ = b.DB.Save(&link).Error
	if on {
		_ = cli.SendMessageMarkup(chatID, "✅ تمدید خودکار روشن شد. پلن تمدید را از منوی کیف پول انتخاب کنید.", b.mainMenu())
	} else {
		_ = cli.SendMessageMarkup(chatID, "تمدید خودکار خاموش شد.", b.mainMenu())
	}
}

func (b *UserBot) setAutoRenewPlan(cli *Client, fromID, chatID int64, planID uint) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil {
		_ = cli.SendMessage(chatID, "حساب تلگرام به اشتراک وصل نیست.")
		return
	}
	var p models.ShopPlan
	if b.DB.First(&p, planID).Error != nil {
		_ = cli.SendMessage(chatID, "پلن نامعتبر")
		return
	}
	link.AutoRenewPlanID = planID
	link.AutoRenewEnabled = true
	_ = b.DB.Save(&link).Error
	_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ پلن تمدید: %s (%s تومان)", p.Name, formatToman(p.PriceToman)), b.mainMenu())
}

func (b *UserBot) sendDevices(cli *Client, fromID, chatID int64) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil {
		_ = cli.SendMessageMarkup(chatID, "اشتراکی وصل نیست.", b.mainMenu())
		return
	}
	var devices []models.ClientDevice
	b.DB.Where("client_id = ?", link.ClientID).Order("active desc, last_seen_at desc").Limit(20).Find(&devices)
	if len(devices) == 0 {
		_ = cli.SendMessageMarkup(chatID, "📱 نشست فعالی ثبت نشده.\nوقتی با اپ به کانفیگ وصل شوید، اینجا نمایش داده می‌شود.", b.mainMenu())
		return
	}
	var activeN int
	for _, d := range devices {
		if d.Active {
			activeN++
		}
	}
	_ = cli.SendMessage(chatID, fmt.Sprintf("📱 <b>دستگاه‌های متصل</b>\nفعال: %d از %d\nدر صورت مشکوک بودن، همان را قطع کنید یا UUID را عوض کنید.", activeN, len(devices)))
	for _, d := range devices {
		st := "✅ فعال"
		if !d.Active {
			st = "⛔ قطع‌شده"
		}
		name := d.Name
		if name == "" {
			name = d.Model
		}
		if name == "" {
			name = d.DeviceID
		}
		if len(name) > 48 {
			name = name[:45] + "…"
		}
		os := d.OS
		if d.OSVersion != "" {
			os += " " + d.OSVersion
		}
		app := d.AppName
		if d.AppVersion != "" {
			app += " " + d.AppVersion
		}
		seen := d.LastSeenAt.Local().Format("2006-01-02 15:04")
		body := fmt.Sprintf("• <b>%s</b>\n%s\n%s · %s\nآخرین اتصال: %s", name, st, os, app, seen)
		var kb *InlineKeyboardMarkup
		if d.Active {
			kb = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "⛔ قطع این دستگاه", CallbackData: fmt.Sprintf("revdev:%d", d.ID)}},
			}}
		}
		_ = cli.SendMessageMarkup(chatID, body, kb)
	}
	_ = cli.SendMessageMarkup(chatID, "عملیات کلی:", &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "⛔ قطع همه نشست‌ها", CallbackData: "revoke_all"}},
		{{Text: "🔑 Reset UUID (کلید جدید)", CallbackData: "reset_uuid"}},
		{{Text: "◀️ منو", CallbackData: "menu"}},
	}})
}

func (b *UserBot) revokeDevice(cli *Client, fromID, chatID int64, deviceID uint) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil {
		return
	}
	now := time.Now()
	b.DB.Model(&models.ClientDevice{}).Where("id = ? AND client_id = ?", deviceID, link.ClientID).
		Updates(map[string]any{"active": false, "revoked_at": now})
	_ = cli.SendMessageMarkup(chatID, "✅ دستگاه قطع شد", b.mainMenu())
}

func (b *UserBot) requestSensitiveConfirm(cli *Client, fromID, chatID int64, kind string) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil || link.ClientID == 0 {
		_ = cli.SendMessageMarkup(chatID, "اشتراکی به تلگرام شما وصل نیست.", b.mainMenu())
		return
	}
	pending, err := shop.CreateSensitivePending(b.DB, fromID, chatID, kind, link.ClientID)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	label := shop.SensitiveKindLabel(kind)
	msg := fmt.Sprintf(`⚠️ <b>تأیید عملیات حساس</b>

عملیات: <b>%s</b>
مهلت: ۱۰ دقیقه

اگر این درخواست از طرف شما نبود، انصراف بزنید.`, label)
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "✅ تأیید می‌کنم", CallbackData: "sens_ok:" + pending.Token}},
		{{Text: "❌ انصراف", CallbackData: "sens_no:" + pending.Token}},
	}}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

func (b *UserBot) confirmSensitive(cli *Client, fromID, chatID int64, token string) {
	pending, err := shop.LoadSensitivePending(b.DB, token, fromID)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	shop.ConsumeSensitivePending(b.DB, token)
	var cl models.Client
	if b.DB.First(&cl, pending.ClientID).Error != nil {
		_ = cli.SendMessageMarkup(chatID, "کلاینت پیدا نشد.", b.mainMenu())
		return
	}
	now := time.Now()
	switch pending.Kind {
	case shop.SensKindRevokeAll:
		_ = b.DB.Transaction(func(tx *gorm.DB) error {
			if _, err := devices.RevokeAll(tx, cl.ID); err != nil {
				return err
			}
			return tx.Model(&models.Client{}).Where("id = ?", cl.ID).Updates(map[string]any{
				"uuid":                       uuid.NewString(),
				"password":                   uuid.NewString(),
				"subscription_token_version": gorm.Expr("subscription_token_version + 1"),
			}).Error
		})
		if b.AfterClientChange != nil {
			b.AfterClientChange()
		}
		_ = cli.SendMessageMarkup(chatID, "✅ همه دستگاه‌ها قطع و کلید جدید صادر شد.\nکانفیگ قبلی دیگر کار نمی‌کند.\nاز «📦 کانفیگ‌های من» لینک تازه بگیرید.", b.mainMenu())
	case shop.SensKindResetUUID:
		_ = b.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.Client{}).Where("id = ?", cl.ID).Updates(map[string]any{
				"uuid":                       uuid.NewString(),
				"password":                   uuid.NewString(),
				"subscription_token_version": gorm.Expr("subscription_token_version + 1"),
			}).Error; err != nil {
				return err
			}
			return tx.Model(&models.ClientDevice{}).Where("client_id = ?", cl.ID).
				Updates(map[string]any{"active": false, "revoked_at": now}).Error
		})
		if b.AfterClientChange != nil {
			b.AfterClientChange()
		}
		_ = b.DB.First(&cl, cl.ID)
		_ = cli.SendMessageMarkup(chatID, "🔑 کلید جدید صادر شد. کانفیگ قبلی دیگر کار نمی‌کند.\nSub: <code>"+cl.SubID+"</code>\nاز «دریافت کانفیگ» لینک تازه بگیرید.", b.mainMenu())
	default:
		_ = cli.SendMessageMarkup(chatID, "نوع عملیات ناشناخته.", b.mainMenu())
	}
}

// sendMyConfigs lists purchase-linked configs for this Telegram user.
func (b *UserBot) sendMyConfigs(cli *Client, fromID, chatID int64) {
	seen := map[uint]bool{}
	var clients []models.Client

	// 1) current linked customer
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error == nil && link.ClientID > 0 {
		var cl models.Client
		if b.DB.First(&cl, link.ClientID).Error == nil {
			clients = append(clients, cl)
			seen[cl.ID] = true
		}
	}
	// 2) clients created via bot (email patterns)
	var byEmail []models.Client
	b.DB.Where("email LIKE ? OR email LIKE ?", fmt.Sprintf("tg%d_%%", fromID), fmt.Sprintf("trial_tg%d_%%", fromID)).
		Order("id desc").Limit(20).Find(&byEmail)
	for _, cl := range byEmail {
		if !seen[cl.ID] {
			clients = append(clients, cl)
			seen[cl.ID] = true
		}
	}
	// 3) approved shop orders with client_id
	var orders []models.ShopOrder
	b.DB.Where("telegram_user_id = ? AND status = ? AND client_id > 0", fromID, "approved").
		Order("id desc").Limit(20).Find(&orders)
	for _, o := range orders {
		if seen[o.ClientID] {
			continue
		}
		var cl models.Client
		if b.DB.First(&cl, o.ClientID).Error == nil {
			clients = append(clients, cl)
			seen[cl.ID] = true
		}
	}

	if len(clients) == 0 {
		_ = cli.SendMessageMarkup(chatID, "هنوز کانفیگی از ربات برای شما ثبت نشده.\nاز «🛒 خرید» یا «🎁 تست رایگان» شروع کنید.", b.mainMenu())
		return
	}

	host := publicHost(b.DB)
	_ = cli.SendMessage(chatID, fmt.Sprintf("📦 <b>کانفیگ‌های شما</b>\n%d مورد:", len(clients)))
	for i, cl := range clients {
		var ib models.Inbound
		_ = b.DB.First(&ib, cl.InboundID)
		share, _, _, _ := shop.BuildClientConfigs(ib, cl, host)
		sub := subURL(b.DB, cl.SubID)
		st := "فعال"
		if !cl.Enable {
			st = "غیرفعال"
		} else if cl.IsExpired() {
			st = "منقضی"
		} else if cl.IsOverQuota() {
			st = "اتمام حجم"
		}
		exp := "∞"
		if cl.ExpiryTime > 0 {
			exp = time.UnixMilli(cl.ExpiryTime).Local().Format("2006-01-02")
		}
		gb := "نامحدود"
		if cl.TotalGB > 0 {
			gb = fmt.Sprintf("%d GB", cl.TotalGB)
		}
		title := cl.SubscriptionName
		if title == "" {
			title = cl.Email
		}
		msg := fmt.Sprintf(`<b>%d) %s</b>
وضعیت: %s · حجم: %s · تا: %s
اینباند: %s
Sub: <code>%s</code>
لینک ساب:
<code>%s</code>`,
			i+1, title, st, gb, exp, ib.Remark, cl.SubID, sub)
		if share != "" {
			msg += "\nکانفیگ:\n<code>" + share + "</code>"
		}
		kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "🔄 بروزرسانی اشتراک", CallbackData: fmt.Sprintf("refreshcfg:%d", cl.ID)}},
			{{Text: "📁 فایل + QR", CallbackData: fmt.Sprintf("cfgfile:%d", cl.ID)}, {Text: "📊 وضعیت", CallbackData: "status"}},
		}}
		_ = cli.SendMessageMarkup(chatID, msg, kb)
	}
	_ = cli.SendMessageMarkup(chatID, "اگر کانفیگ قطع یا عوض شد، «🔄 بروزرسانی اشتراک» را بزنید تا لینک ساب و کانفیگ تازه از پنل گرفته شود.", b.mainMenu())
}

func (b *UserBot) checkChannelJoin(cli *Client, fromID, chatID int64) {
	if !ChannelEnforceOn(b.DB) {
		_ = cli.SendMessageMarkup(chatID, "اجبار عضویت فعلاً خاموش است.", b.mainMenu())
		return
	}
	if ChannelExemptActive(b.DB) && UserHasActiveSubscription(b.DB, fromID) {
		_ = cli.SendMessageFull(chatID, "✅ شما اشتراک فعال دارید و از عضویت اجباری معاف هستید.", b.mainMenu(), b.replyMainKeyboard())
		return
	}
	chs := RequiredChannelList(b.DB)
	missing := MissingChannels(cli, chs, fromID)
	if len(missing) == 0 {
		_ = cli.SendMessageFull(chatID, "✅ عضویت در همه کانال‌ها تأیید شد. می‌توانید از ربات استفاده کنید.", b.mainMenu(), b.replyMainKeyboard())
		return
	}
	var sb strings.Builder
	sb.WriteString(MsgChannelStill + "\n\nکانال‌های باقی‌مانده:\n")
	for _, ch := range missing {
		sb.WriteString("• <code>" + ch + "</code>\n")
	}
	_ = cli.SendMessageMarkup(chatID, sb.String(), channelJoinKeyboard(missing))
}

// refreshConfig reloads client+inbound from panel DB and sends fresh sub URL + share link.
func (b *UserBot) refreshConfig(cli *Client, fromID, chatID int64, clientID uint) {
	cl, ok := b.clientOwnedByTG(fromID, clientID)
	if !ok {
		_ = cli.SendMessageMarkup(chatID, "این کانفیگ متعلق به شما نیست یا پیدا نشد.", b.mainMenu())
		return
	}
	var ib models.Inbound
	if b.DB.First(&ib, cl.InboundID).Error != nil {
		_ = cli.SendMessageMarkup(chatID, "اینباند مرتبط پیدا نشد. با پشتیبانی تماس بگیرید.", b.mainMenu())
		return
	}
	// Re-read client to get latest UUID/password/enable after panel changes
	_ = b.DB.First(&cl, cl.ID)
	host := publicHost(b.DB)
	share, _, _, err := shop.BuildClientConfigs(ib, cl, host)
	sub := subURL(b.DB, cl.SubID)
	st := "فعال"
	if !cl.Enable {
		st = "غیرفعال"
	} else if cl.IsExpired() {
		st = "منقضی"
	} else if cl.IsOverQuota() {
		st = "اتمام حجم"
	}
	title := cl.SubscriptionName
	if title == "" {
		title = cl.Email
	}
	msg := fmt.Sprintf(`🔄 <b>بروزرسانی اشتراک</b>

%s
وضعیت: <b>%s</b>
اینباند: %s

لینک ساب (جایگزین کنید):
<code>%s</code>
`, title, st, ib.Remark, sub)
	if share != "" {
		msg += "\nکانفیگ به‌روز:\n<code>" + share + "</code>\n"
	}
	if err != nil {
		msg += "\n⚠️ ساخت لینک کانفیگ: " + err.Error()
	}
	msg += "\nدر اپ، ساب قدیمی را حذف و این لینک را Import کنید؛ یا کانفیگ را جایگزین کنید."
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🔄 دوباره بروزرسانی", CallbackData: fmt.Sprintf("refreshcfg:%d", cl.ID)}},
		{{Text: "📁 فایل + QR", CallbackData: fmt.Sprintf("cfgfile:%d", cl.ID)}},
		{{Text: "📦 کانفیگ‌های من", CallbackData: "myconfigs"}},
	}}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

func (b *UserBot) clientOwnedByTG(fromID int64, clientID uint) (models.Client, bool) {
	var cl models.Client
	if b.DB.First(&cl, clientID).Error != nil {
		return cl, false
	}
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ? AND client_id = ?", fromID, clientID).First(&link).Error == nil {
		return cl, true
	}
	prefix := fmt.Sprintf("tg%d_", fromID)
	trial := fmt.Sprintf("trial_tg%d_", fromID)
	if strings.HasPrefix(cl.Email, prefix) || strings.HasPrefix(cl.Email, trial) {
		return cl, true
	}
	var n int64
	b.DB.Model(&models.ShopOrder{}).Where("telegram_user_id = ? AND client_id = ? AND status = ?", fromID, clientID, "approved").Count(&n)
	return cl, n > 0
}

func (b *UserBot) sendConfigFileForClient(cli *Client, fromID, chatID int64, clientID uint) {
	cl, ok := b.clientOwnedByTG(fromID, clientID)
	if !ok {
		_ = cli.SendMessageMarkup(chatID, "کانفیگ پیدا نشد.", b.mainMenu())
		return
	}
	// reuse existing sendConfigFiles path if it uses linked client only — send refreshed bundle
	var ib models.Inbound
	_ = b.DB.First(&ib, cl.InboundID)
	host := publicHost(b.DB)
	share, textBundle, sbJSON, err := shop.BuildClientConfigs(ib, cl, host)
	if err != nil {
		_ = cli.SendMessage(chatID, "خطا: "+err.Error())
		return
	}
	sub := subURL(b.DB, cl.SubID)
	_ = cli.SendMessage(chatID, fmt.Sprintf("📁 کانفیگ تازه\nSub:\n<code>%s</code>\n\nLink:\n<code>%s</code>", sub, share))
	if len(sbJSON) > 0 {
		_ = cli.SendDocumentBytes(chatID, shop.ConfigFilename(cl), sbJSON, "sing-box / JSON")
	}
	if textBundle != "" && textBundle != share+"\n" {
		_ = cli.SendMessage(chatID, textBundle)
	}
	// QR if available
	if png, e := shop.QRCodePNG(share, 256); e == nil && len(png) > 0 {
		_ = cli.SendPhotoBytes(chatID, "qr.png", png, "اسکن برای افزودن")
	}
}

func (b *UserBot) sendTransactions(cli *Client, fromID, chatID int64) {
	orders := shop.ListUserOrders(b.DB, fromID, 15)
	ledger := shop.ListWalletLedger(b.DB, fromID, 15)
	var sb strings.Builder
	sb.WriteString("🧾 <b>تاریخچه مالی</b>\n\n<b>سفارش‌ها / فاکتورها</b>\n")
	if len(orders) == 0 {
		sb.WriteString("— موردی نیست\n")
	}
	for _, o := range orders {
		sb.WriteString(fmt.Sprintf("• #%d %s · %s ت · <b>%s</b>\n", o.ID, shop.EscText(o.PlanName), formatToman(o.PriceToman), shop.EscText(o.Status)))
	}
	sb.WriteString("\n<b>کیف پول</b>\n")
	if len(ledger) == 0 {
		sb.WriteString("— موردی نیست\n")
	}
	for _, l := range ledger {
		sign := ""
		if l.DeltaToman > 0 {
			sign = "+"
		}
		sb.WriteString(fmt.Sprintf("• %s%s ت · %s · موجودی %s\n", sign, formatToman(l.DeltaToman), shop.EscText(l.Kind), formatToman(l.BalanceAfter)))
	}
	_ = cli.SendMessageMarkup(chatID, sb.String(), b.mainMenu())
}

func (b *UserBot) startCustomGB(cli *Client, fromID, chatID int64) {
	cfg := shop.LoadCustomGBConfig(b.DB)
	if !cfg.Enabled || cfg.PricePerGB <= 0 {
		_ = cli.SendMessageMarkup(chatID, "خرید حجم دلخواه فعلاً فعال نیست. از منوی پلن‌ها استفاده کنید.", b.mainMenu())
		return
	}
	SetPendingCustomGB(fromID, true)
	_ = cli.SendMessage(chatID, fmt.Sprintf(`📦 <b>خرید حجم دلخواه (ماهانه)</b>

قیمت هر گیگ: <b>%s تومان</b>
بازه مجاز: %d تا %d گیگ
مدت: %d روز

چند گیگ می‌خواهید؟ فقط عدد را بفرستید (مثال: 40)
/cancel برای انصراف`, formatToman(cfg.PricePerGB), cfg.MinGB, cfg.MaxGB, cfg.Days))
}

func (b *UserBot) quoteCustomGB(cli *Client, fromID, chatID int64, name string, gb int) {
	cfg := shop.LoadCustomGBConfig(b.DB)
	price, err := shop.CalcCustomGBPrice(cfg, gb)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, err.Error(), b.mainMenu())
		return
	}
	w, _ := shop.GetOrCreateWallet(b.DB, fromID)
	msg := fmt.Sprintf(`📦 <b>پیش‌فاکتور حجم دلخواه</b>

حجم: <b>%d GB</b> · مدت: %d روز
قیمت هر گیگ: %s ت
مبلغ کل: <b>%s تومان</b>
موجودی کیف پول: %s ت

روش پرداخت را انتخاب کنید:`, gb, cfg.Days, formatToman(cfg.PricePerGB), formatToman(price), formatToman(w.BalanceToman))
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "💳 کارت‌به‌کارت", CallbackData: fmt.Sprintf("cgb_card:%d", gb)}},
		{{Text: "⚡ پرداخت از کیف پول (آنی)", CallbackData: fmt.Sprintf("cgb_wallet:%d", gb)}},
		{{Text: "◀️ منو", CallbackData: "menu"}},
	}}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

func (b *UserBot) orderCustomGB(cli *Client, fromID, chatID int64, name string, gb int, fromWallet bool) {
	cfg := shop.LoadCustomGBConfig(b.DB)
	price, err := shop.CalcCustomGBPrice(cfg, gb)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, err.Error(), b.mainMenu())
		return
	}
	ibID, err := shop.ResolveCustomInbound(b.DB, cfg)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, err.Error(), b.mainMenu())
		return
	}
	planName := fmt.Sprintf("حجم دلخواه %dGB / %dروز", gb, cfg.Days)
	order := models.ShopOrder{
		TelegramUserID: fromID, TelegramChatID: chatID, TelegramName: name,
		PlanName: planName, TotalGB: int64(gb), DurationDays: cfg.Days,
		PriceToman: price, InboundID: ibID, Status: "pending_payment", Kind: "plan",
	}
	if fromWallet {
		order.Status = "pending_review"
		cl, err := shop.DebitAndFulfill(b.DB, &order, "buy", "custom_gb", planName)
		if err != nil {
			_ = cli.SendMessageMarkup(chatID, "خطا: "+err.Error()+"\nدر صورت کسر اشتباه با پشتیبانی تماس بگیرید.", b.mainMenu())
			return
		}
		// need xray restart - best effort via note; panel workers may handle
		share := ""
		_ = share
		if b.AfterClientChange != nil {
			b.AfterClientChange()
		}
		_ = cli.SendMessageMarkup(chatID, fmt.Sprintf("✅ پرداخت از کیف پول انجام شد.\nحجم %dGB فعال شد.\nSub: <code>%s</code>\nاز «📦 کانفیگ‌های من» لینک را بگیرید.", gb, cl.SubID), b.mainMenu())
		return
	}
	// card-to-card
	_ = b.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status = ? AND kind = ?", fromID, "pending_payment", "plan").
		Update("status", "cancelled")
	if err := b.DB.Create(&order).Error; err != nil {
		_ = cli.SendMessageMarkup(chatID, "خطا در ثبت سفارش.", b.mainMenu())
		return
	}
	card, holder, inst, _ := shop.PaymentInfo(b.DB)
	msg := fmt.Sprintf(`🧾 سفارش #%d
%s
مبلغ: <b>%s تومان</b>

💳 کارت: <code>%s</code>
%s
%s

پس از واریز، <b>عکس رسید</b> را همین‌جا ارسال کنید.`, order.ID, planName, formatToman(price), card, holder, inst)
	_ = cli.SendMessageMarkup(chatID, msg, b.mainMenu())
}

func (b *UserBot) startSupport(cli *Client, fromID, chatID int64) {
	SetUserComposingTicket(fromID, true)
	_ = cli.SendMessageMarkup(chatID, `🎫 <b>پشتیبانی</b>
متن مشکل را بنویسید (یا عکس بفرستید).
پیام به ادمین می‌رسد و پاسخ همین‌جا نمایش داده می‌شود.

برای انصراف: /cancel`, &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "📋 تیکت‌های من", CallbackData: "mytickets"}},
		{{Text: "🏠 منو", CallbackData: "menu"}},
	}})
}

func (b *UserBot) listMyTickets(cli *Client, fromID, chatID int64) {
	var rows []models.Ticket
	b.DB.Where("telegram_user_id = ?", fromID).Order("id desc").Limit(10).Find(&rows)
	if len(rows) == 0 {
		_ = cli.SendMessageMarkup(chatID, "تیکتی ندارید.\nاز «🎫 پشتیبانی» تیکت جدید باز کنید.", &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "🎫 پشتیبانی جدید", CallbackData: "support"}},
			{{Text: "🏠 منو", CallbackData: "menu"}},
		}})
		return
	}
	var sb strings.Builder
	sb.WriteString("🎫 <b>تیکت‌های شما</b>\n")
	rowsKB := [][]InlineKeyboardButton{}
	for _, t := range rows {
		sb.WriteString(fmt.Sprintf("• #%d %s — <b>%s</b>\n", t.ID, shop.EscText(t.Subject), t.Status))
		if t.Status != "closed" {
			rowsKB = append(rowsKB, []InlineKeyboardButton{{Text: fmt.Sprintf("↩️ پاسخ #%d", t.ID), CallbackData: fmt.Sprintf("ticket_reply:%d", t.ID)}})
		}
	}
	rowsKB = append(rowsKB,
		[]InlineKeyboardButton{{Text: "🎫 تیکت جدید", CallbackData: "support"}},
		[]InlineKeyboardButton{{Text: "🏠 منو", CallbackData: "menu"}},
	)
	_ = cli.SendMessageMarkup(chatID, sb.String(), &InlineKeyboardMarkup{InlineKeyboard: rowsKB})
}

func (b *UserBot) giveTrial(cli *Client, fromID, chatID int64) {
	cfg := shop.LoadTrialConfig(b.DB)
	if !cfg.Enabled {
		_ = cli.SendMessageMarkup(chatID, "🎁 تست رایگان فعلاً فعال نیست.\nاز منوی خرید پلن تهیه کنید.", b.mainMenu())
		return
	}
	if shop.HasUsedTrial(b.DB, fromID) {
		_ = cli.SendMessageMarkup(chatID, "ℹ️ هر اکانت تلگرام فقط <b>یک‌بار</b> می‌تواند تست رایگان بگیرد.\nبرای ادامه از «🛒 خرید» استفاده کنید.", b.mainMenu())
		return
	}
	_ = cli.SendMessage(chatID, "⏳ در حال ساخت کانفیگ تست…")
	cl, err := shop.CreateTrialAccount(b.DB, fromID, chatID, cfg)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	if b.AfterClientChange != nil {
		b.AfterClientChange()
	}
	host := publicHost(b.DB)
	var ib models.Inbound
	_ = b.DB.First(&ib, cl.InboundID)
	share, _, _, _ := shop.BuildClientConfigs(ib, cl, host)
	sub := subURL(b.DB, cl.SubID)
	msg := fmt.Sprintf(`🎁 <b>تست رایگان فعال شد</b>

حجم: <b>%d مگابایت</b>
مدت: <b>%d ساعت</b>
هر اکانت فقط یک‌بار

Sub: <code>%s</code>
لینک ساب:
<code>%s</code>
`, cfg.MB, cfg.Hours, cl.SubID, sub)
	if share != "" {
		msg += "\nکانفیگ:\n<code>" + share + "</code>\n"
	}
	msg += "\nپس از اتمام زمان یا حجم، اکانت <b>خودکار</b> قطع می‌شود.\nاگر کیفیت مناسب بود از «🛒 خرید» اشتراک بگیرید."
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🔄 بروزرسانی اشتراک", CallbackData: fmt.Sprintf("refreshcfg:%d", cl.ID)}},
		{{Text: "📁 فایل + QR", CallbackData: fmt.Sprintf("cfgfile:%d", cl.ID)}},
		{{Text: "🛒 خرید اشتراک", CallbackData: "shop"}},
		{{Text: "◀️ منو", CallbackData: "menu"}},
	}}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
	b.sendConfigFileForClient(cli, fromID, chatID, cl.ID)
}

func (b *UserBot) sendConfigFiles(cli *Client, fromID, chatID int64) {
	var link models.TelegramCustomer
	if b.DB.Where("telegram_user_id = ?", fromID).First(&link).Error != nil {
		_ = cli.SendMessageMarkup(chatID, "اشتراکی به تلگرام شما وصل نیست. اول خرید یا تست بگیرید.", b.mainMenu())
		return
	}
	var cl models.Client
	if b.DB.First(&cl, link.ClientID).Error != nil {
		_ = cli.SendMessage(chatID, "کلاینت پیدا نشد.")
		return
	}
	var ib models.Inbound
	_ = b.DB.First(&ib, cl.InboundID)
	host := publicHost(b.DB)
	share, textBundle, sbJSON, err := shop.BuildClientConfigs(ib, cl, host)
	if err != nil {
		_ = cli.SendMessage(chatID, "خطا در ساخت کانفیگ: "+err.Error())
		return
	}
	_ = cli.SendMessage(chatID, "📁 فایل‌های کانفیگ برای شما ارسال می‌شود…")
	_ = cli.SendDocumentBytes(chatID, shop.ConfigFilename(cl), sbJSON, "sing-box / JSON config")
	_ = cli.SendDocumentBytes(chatID, "subscription.txt", []byte(textBundle), "Share link / subscription line")
	if png, err := shop.QRCodePNG(share, 280); err == nil {
		_ = cli.SendPhotoBytes(chatID, "qr.png", png, "QR Code — با اپ اسکن کنید")
	}
	_ = cli.SendMessageMarkup(chatID, "✅ ارسال شد.\nSub: <code>"+cl.SubID+"</code>", b.mainMenu())
}

func publicHost(db *gorm.DB) string {
	// Prefer panel setting or empty (BuildShareLink validates)
	h := GetSetting(db, "public_host")
	if h == "" {
		h = GetSetting(db, "ELECTRU_PUBLIC_HOST")
	}
	return h
}

func subURL(db *gorm.DB, subID string) string {
	base := GetSetting(db, "subscription_base_url")
	if base == "" {
		return "/sub/" + subID
	}
	return strings.TrimRight(base, "/") + "/sub/" + subID
}

func (b *UserBot) notifyAdminsNewOrder(order models.ShopOrder) {
	token := GetSetting(b.DB, KeyAdminToken)
	ids := ParseAdminIDs(GetSetting(b.DB, KeyAdminIDs))
	if token == "" || len(ids) == 0 {
		return
	}
	cli := NewClient(token)
	text := fmt.Sprintf("🛒 سفارش جدید #%d\n%s\n%s تومان\nکاربر: %s (tg %d)\nوضعیت: در انتظار تأیید\nمی‌توانید از دکمه‌ها یا پنل تأیید کنید.",
		order.ID, order.PlanName, formatToman(order.PriceToman), order.TelegramName, order.TelegramUserID)
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{
			{Text: "✅ تأیید", CallbackData: fmt.Sprintf("adm:ok:%d", order.ID)},
			{Text: "❌ رد", CallbackData: fmt.Sprintf("adm:no:%d", order.ID)},
		},
		{{Text: "📋 لیست سفارش‌ها", CallbackData: "adm:orders"}},
	}}
	for id := range ids {
		_ = cli.SendMessageMarkup(id, text, kb)
	}
}

func (b *UserBot) handleCloudBackup(cli *Client, fromID, chatID int64) {
	code, snap, err := CreateOrRotateCloudBackup(b.DB, fromID, chatID)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	msg := "☁️ <b>پشتیبان ابری ساخته شد</b>\n\n"
	msg += "کد بازیابی شما:\n<code>" + code + "</code>\n\n"
	msg += "⚠️ <b>این کد را در جای امن نگه دارید.</b> با آن می‌توانید بعد از تعویض گوشی یا ریست تلگرام، تمام اکانت‌ها را برگردانید.\n"
	msg += fmt.Sprintf("⏱ اعتبار کد: <b>%d روز</b>\n", models.CloudBackupDefaultTTLDays)
	msg += "هر بار «پشتیبان ابری» را بزنید، کد قبلی باطل و کد جدید ساخته می‌شود.\n\n"
	msg += formatCloudSnapshotSummary(snap)
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🚫 باطل کردن کد", CallbackData: "cloud_revoke"}},
		{{Text: "♻️ بازیابی با کد دیگر", CallbackData: "cloud_restore"}},
		{{Text: "🏠 منوی اصلی", CallbackData: "status"}},
	}}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

func (b *UserBot) startCloudRestore(cli *Client, fromID, chatID int64) {
	SetPendingRestore(fromID, true)
	_ = cli.SendMessageMarkup(chatID, "♻️ <b>بازیابی اکانت</b>\n\nکد پشتیبان را بفرستید (مثال: <code>EV-AB12-CD34EF</code>).\n\nاگر گوشی عوض کرده‌اید یا تلگرام را ریست کرده‌اید، با همین کد اشتراک‌ها دوباره به این حساب وصل می‌شوند.\n\n/cancel برای انصراف", b.mainMenu())
}

func (b *UserBot) handleCloudRevoke(cli *Client, fromID, chatID int64) {
	n, err := RevokeCloudBackup(b.DB, fromID)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	if n == 0 {
		_ = cli.SendMessageMarkup(chatID, "کد فعالی برای باطل کردن وجود ندارد.", b.mainMenu())
		return
	}
	_ = cli.SendMessageMarkup(chatID, "✅ کد پشتیبان ابری باطل شد. برای ساخت کد جدید دوباره «پشتیبان ابری» را بزنید.", b.mainMenu())
}
