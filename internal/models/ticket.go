package models

import "time"

// Ticket is a support conversation between an end-user (identified by client
// SubID or email) and panel operators.
type Ticket struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	ClientID       uint       `gorm:"index" json:"clientId"`
	SubID          string     `gorm:"size:32;index" json:"subId,omitempty"`
	Email          string     `gorm:"size:128;index" json:"email"`
	Subject        string     `gorm:"size:255" json:"subject"`
	Status         string     `gorm:"size:24;default:open;index" json:"status"` // open | pending | closed
	Priority       string     `gorm:"size:16;default:normal" json:"priority"`   // low | normal | high
	CreatedBy      string     `gorm:"size:64" json:"createdBy"`                 // client | admin
	TelegramUserID int64      `gorm:"index" json:"telegramUserId,omitempty"`
	TelegramChatID int64      `gorm:"index" json:"telegramChatId,omitempty"`
	AssignedTo     uint       `gorm:"index" json:"assignedTo,omitempty"`
	LastReplyAt    *time.Time `json:"lastReplyAt,omitempty"`
	ClosedAt       *time.Time `json:"closedAt,omitempty"`
	MessageCount   int        `gorm:"default:0" json:"messageCount"`
	CreatedAt      time.Time  `gorm:"index" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"index" json:"updatedAt"`
}

// TicketMessage is one message in a ticket thread.
type TicketMessage struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TicketID    uint      `gorm:"index" json:"ticketId"`
	AuthorType  string    `gorm:"size:16" json:"authorType"` // client | admin | system
	AuthorName  string    `gorm:"size:64" json:"authorName"`
	AdminID     uint      `json:"adminId,omitempty"`
	Body        string    `gorm:"type:text" json:"body"`
	MediaFileID string    `gorm:"size:256" json:"mediaFileId,omitempty"` // Telegram file_id when available
	MediaKind   string    `gorm:"size:16" json:"mediaKind,omitempty"`    // photo | document
	CreatedAt   time.Time `json:"createdAt"`
}
