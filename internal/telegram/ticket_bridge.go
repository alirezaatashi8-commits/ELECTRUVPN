package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// pendingAdminReply maps admin telegram user id → ticket id awaiting reply text.
// pendingUserTicket maps user telegram id → true when composing a new ticket body.
var (
	replyMu            sync.Mutex
	pendingReply       = map[int64]uint{}
	userTicketMu       sync.Mutex
	pendingUserTicket  = map[int64]bool{}
	pendingUserReplyTo = map[int64]uint{} // optional: append to specific ticket
)

func SetPendingTicketReply(adminTG int64, ticketID uint) {
	replyMu.Lock()
	pendingReply[adminTG] = ticketID
	replyMu.Unlock()
}

func TakePendingTicketReply(adminTG int64) (uint, bool) {
	replyMu.Lock()
	defer replyMu.Unlock()
	id, ok := pendingReply[adminTG]
	if ok {
		delete(pendingReply, adminTG)
	}
	return id, ok
}

func SetUserComposingTicket(tgID int64, on bool) {
	userTicketMu.Lock()
	if on {
		pendingUserTicket[tgID] = true
	} else {
		delete(pendingUserTicket, tgID)
		delete(pendingUserReplyTo, tgID)
	}
	userTicketMu.Unlock()
}

func SetUserReplyToTicket(tgID int64, ticketID uint) {
	userTicketMu.Lock()
	pendingUserTicket[tgID] = true
	pendingUserReplyTo[tgID] = ticketID
	userTicketMu.Unlock()
}

func IsUserComposingTicket(tgID int64) bool {
	userTicketMu.Lock()
	defer userTicketMu.Unlock()
	return pendingUserTicket[tgID]
}

func TakeUserReplyTicket(tgID int64) (uint, bool) {
	userTicketMu.Lock()
	defer userTicketMu.Unlock()
	id, ok := pendingUserReplyTo[tgID]
	delete(pendingUserTicket, tgID)
	delete(pendingUserReplyTo, tgID)
	return id, ok && id > 0
}

// supportAdminIDs returns telegram IDs allowed to see tickets (owner/admin/support).
func supportAdminIDs(db *gorm.DB) map[int64]bool {
	out := map[int64]bool{}
	roles := ParseAdminRoles(GetSetting(db, KeyAdminRoles))
	ids := ParseAdminIDs(GetSetting(db, KeyAdminIDs))
	for id := range ids {
		role := roles[id]
		if role == "" {
			role = "owner"
		}
		if RoleAllowed(role, "tickets") {
			out[id] = true
		}
	}
	// roles map may include IDs not in adminIds
	for id, role := range roles {
		if RoleAllowed(role, "tickets") {
			out[id] = true
		}
	}
	return out
}

// NotifyAdminsNewTicket pushes ticket text (+ optional media via user-bot download) to support admins.
func NotifyAdminsNewTicket(db *gorm.DB, t models.Ticket, body string) {
	NotifyAdminsTicketMedia(db, t, body, "", "")
}

func NotifyAdminsTicketMedia(db *gorm.DB, t models.Ticket, body, mediaFileID, mediaKind string) {
	token := GetSetting(db, KeyAdminToken)
	ids := supportAdminIDs(db)
	if token == "" || len(ids) == 0 {
		return
	}
	adminCli := NewClient(token)
	text := fmt.Sprintf("🎫 تیکت <b>#%d</b>\nموضوع: %s\nاز: %s\nTG: <code>%d</code>\nوضعیت: %s\n\n%s",
		t.ID, t.Subject, t.Email, t.TelegramUserID, t.Status, body)
	if len(text) > 3500 {
		text = text[:3500] + "…"
	}
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "💬 پاسخ", CallbackData: fmt.Sprintf("adm:treply:%d", t.ID)}},
		{{Text: "✅ بستن", CallbackData: fmt.Sprintf("adm:tclose:%d", t.ID)}},
	}}

	// Relay screenshot: download with user bot, re-upload to admin bot
	var photo []byte
	var photoName string
	if mediaFileID != "" {
		userTok := GetSetting(db, KeyUserToken)
		if userTok != "" {
			if data, name, err := NewClient(userTok).DownloadFile(mediaFileID); err == nil {
				photo, photoName = data, name
				if photoName == "" || !strings.Contains(photoName, ".") {
					photoName = "screenshot.jpg"
				}
			}
		}
	}

	for id := range ids {
		if len(photo) > 0 {
			_ = adminCli.SendPhotoBytes(id, photoName, photo, text)
			_ = adminCli.SendMessageMarkup(id, "⬆️ اسکرین‌شات تیکت #"+fmt.Sprint(t.ID)+" — از دکمه‌ها پاسخ دهید:", kb)
		} else {
			_ = adminCli.SendMessageMarkup(id, text, kb)
		}
	}
}

func (b *AdminBot) startTicketReply(cli *Client, adminTG, chatID int64, ticketID uint) {
	var t models.Ticket
	if b.DB.First(&t, ticketID).Error != nil {
		_ = cli.SendMessage(chatID, "تیکت پیدا نشد")
		return
	}
	SetPendingTicketReply(adminTG, ticketID)
	_ = cli.SendMessage(chatID, fmt.Sprintf("✏️ پاسخ تیکت #%d را بنویسید.\nپیام بعدی شما مستقیم برای کاربر ارسال می‌شود.\nبرای انصراف: /cancel", ticketID))
}

func (b *AdminBot) handleTicketReplyText(cli *Client, adminTG, chatID int64, text string) bool {
	// cancel
	if strings.EqualFold(strings.TrimSpace(text), "/cancel") {
		if _, ok := TakePendingTicketReply(adminTG); ok {
			_ = cli.SendMessage(chatID, "پاسخ لغو شد.")
			return true
		}
		return false
	}
	ticketID, ok := TakePendingTicketReply(adminTG)
	if !ok {
		return false
	}
	var t models.Ticket
	if b.DB.First(&t, ticketID).Error != nil {
		_ = cli.SendMessage(chatID, "تیکت پیدا نشد")
		return true
	}
	body := strings.TrimSpace(text)
	msg := models.TicketMessage{
		TicketID: t.ID, AuthorType: "admin", AuthorName: "admin-tg", Body: body,
	}
	_ = b.DB.Create(&msg).Error
	now := time.Now()
	_ = b.DB.Model(&t).Updates(map[string]any{
		"last_reply_at": now, "message_count": t.MessageCount + 1, "status": "pending",
	}).Error

	delivered := deliverTicketToUser(b.DB, t, fmt.Sprintf("💬 <b>پاسخ پشتیبانی</b> (تیکت #%d)\n\n%s", t.ID, body))
	if delivered {
		_ = cli.SendMessage(chatID, fmt.Sprintf("✅ پاسخ تیکت #%d به کاربر ارسال شد.", ticketID))
	} else {
		_ = cli.SendMessage(chatID, fmt.Sprintf("⚠️ ذخیره شد ولی چت تلگرام کاربر پیدا نشد (تیکت #%d).", ticketID))
	}
	AuditTelegram(b.DB, adminTG, AdminRole(b.DB, adminTG), "tg.ticket.reply", fmt.Sprint(ticketID), map[string]any{"len": len(body)})
	return true
}

func deliverTicketToUser(db *gorm.DB, t models.Ticket, html string) bool {
	ut := GetSetting(db, KeyUserToken)
	if ut == "" {
		return false
	}
	chatID := t.TelegramChatID
	if chatID == 0 && t.TelegramUserID != 0 {
		chatID = t.TelegramUserID
	}
	if chatID == 0 && t.ClientID > 0 {
		var link models.TelegramCustomer
		if db.Where("client_id = ?", t.ClientID).First(&link).Error == nil {
			chatID = link.TelegramChatID
			if chatID == 0 {
				chatID = link.TelegramUserID
			}
		}
	}
	if chatID == 0 && t.SubID != "" {
		var link models.TelegramCustomer
		if db.Where("sub_id = ?", t.SubID).First(&link).Error == nil {
			chatID = link.TelegramChatID
			if chatID == 0 {
				chatID = link.TelegramUserID
			}
		}
	}
	if chatID == 0 {
		return false
	}
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "↩️ پاسخ به پشتیبانی", CallbackData: fmt.Sprintf("ticket_reply:%d", t.ID)}},
		{{Text: "🎫 تیکت‌های من", CallbackData: "mytickets"}},
	}}
	return NewClient(ut).SendMessageMarkup(chatID, html, kb) == nil
}

func (b *AdminBot) closeTicketCmd(cli *Client, chatID int64, ticketID uint) {
	now := time.Now()
	var t models.Ticket
	if b.DB.First(&t, ticketID).Error != nil {
		_ = cli.SendMessage(chatID, "تیکت پیدا نشد")
		return
	}
	_ = b.DB.Model(&t).Updates(map[string]any{"status": "closed", "closed_at": now}).Error
	_ = deliverTicketToUser(b.DB, t, fmt.Sprintf("🔒 تیکت #%d بسته شد. در صورت نیاز تیکت جدید باز کنید.", ticketID))
	SendTicketClosedSurvey(b.DB, t)
	_ = cli.SendMessage(chatID, fmt.Sprintf("تیکت #%d بسته شد.", ticketID))
}

func parseTicketCB(data, prefix string) (uint, bool) {
	if !strings.HasPrefix(data, prefix) {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(data, prefix), 10, 64)
	return uint(n), err == nil
}

// CreateUserTicket opens a ticket from the user bot.
func CreateUserTicket(db *gorm.DB, tgUserID, chatID int64, subject, body, mediaFileID, mediaKind string) (models.Ticket, error) {
	email := fmt.Sprintf("tg:%d", tgUserID)
	subID := ""
	var clientID uint
	var link models.TelegramCustomer
	if db.Where("telegram_user_id = ?", tgUserID).First(&link).Error == nil {
		clientID = link.ClientID
		subID = link.SubID
		if link.Email != "" {
			email = link.Email
		}
	}
	if subject == "" {
		subject = "پشتیبانی ربات"
	}
	t := models.Ticket{
		ClientID: clientID, SubID: subID, Email: email,
		Subject: subject, Status: "open", Priority: "normal",
		CreatedBy: "client", TelegramUserID: tgUserID, TelegramChatID: chatID,
		MessageCount: 1,
	}
	if err := db.Create(&t).Error; err != nil {
		return t, err
	}
	msg := models.TicketMessage{
		TicketID: t.ID, AuthorType: "client", AuthorName: email,
		Body: body, MediaFileID: mediaFileID, MediaKind: mediaKind,
	}
	_ = db.Create(&msg).Error
	now := time.Now()
	_ = db.Model(&t).Update("last_reply_at", now).Error
	NotifyAdminsTicketMedia(db, t, body, mediaFileID, mediaKind)
	return t, nil
}

// AppendUserTicketMessage adds a follow-up from the user (text or screenshot).
func AppendUserTicketMessage(db *gorm.DB, ticketID uint, tgUserID int64, body, mediaFileID, mediaKind string) error {
	var t models.Ticket
	if err := db.First(&t, ticketID).Error; err != nil {
		return err
	}
	if t.TelegramUserID != 0 && t.TelegramUserID != tgUserID {
		return fmt.Errorf("forbidden")
	}
	if t.Status == "closed" {
		t.Status = "open"
		t.ClosedAt = nil
		_ = db.Save(&t).Error
	}
	msg := models.TicketMessage{
		TicketID: t.ID, AuthorType: "client", AuthorName: fmt.Sprintf("tg:%d", tgUserID),
		Body: body, MediaFileID: mediaFileID, MediaKind: mediaKind,
	}
	_ = db.Create(&msg).Error
	now := time.Now()
	_ = db.Model(&t).Updates(map[string]any{
		"last_reply_at": now, "message_count": t.MessageCount + 1, "status": "open",
	}).Error
	NotifyAdminsTicketMedia(db, t, body, mediaFileID, mediaKind)
	return nil
}
