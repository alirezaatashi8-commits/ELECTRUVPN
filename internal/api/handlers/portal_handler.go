package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
)

// PortalHandler serves the browser user portal (independent of Telegram Mini App).
type PortalHandler struct{ S *core.Server }

func NewPortalHandler(s *core.Server) *PortalHandler { return &PortalHandler{S: s} }

const portalCookie = "electru_portal"
const portalTTL = 7 * 24 * time.Hour

type portalClaims struct {
	ClientID uint   `json:"cid"`
	SubID    string `json:"sid"`
	Exp      int64  `json:"exp"`
}

func (h *PortalHandler) signClaims(cl portalClaims) string {
	secret := h.S.Config.JWTSecret
	b, _ := json.Marshal(cl)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(secret+"|portal"))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

func (h *PortalHandler) parseToken(tok string) (portalClaims, error) {
	var zero portalClaims
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		return zero, fmt.Errorf("invalid token")
	}
	mac := hmac.New(sha256.New, []byte(h.S.Config.JWTSecret+"|portal"))
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return zero, fmt.Errorf("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, err
	}
	var cl portalClaims
	if err := json.Unmarshal(raw, &cl); err != nil {
		return zero, err
	}
	if cl.Exp < time.Now().Unix() || cl.ClientID == 0 || cl.SubID == "" {
		return zero, fmt.Errorf("expired")
	}
	return cl, nil
}

func (h *PortalHandler) authClient(c *gin.Context) (models.Client, bool) {
	tok, _ := c.Cookie(portalCookie)
	if tok == "" {
		h := c.GetHeader("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if tok == "" {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return models.Client{}, false
	}
	claims, err := h.parseToken(tok)
	if err != nil {
		c.JSON(401, gin.H{"success": false, "msg": "session expired"})
		return models.Client{}, false
	}
	var cl models.Client
	if h.S.DB.Where("id = ? AND sub_id = ?", claims.ClientID, claims.SubID).First(&cl).Error != nil {
		c.JSON(401, gin.H{"success": false, "msg": "account not found"})
		return models.Client{}, false
	}
	if cl.SubscriptionStatus == "revoked" || (!cl.Enable && cl.SubscriptionRevokedAt != nil) {
		c.JSON(403, gin.H{"success": false, "msg": "subscription revoked"})
		return models.Client{}, false
	}
	return cl, true
}

func (h *PortalHandler) issueSession(c *gin.Context, cl models.Client) string {
	claims := portalClaims{
		ClientID: cl.ID, SubID: cl.SubID,
		Exp: time.Now().Add(portalTTL).Unix(),
	}
	tok := h.signClaims(claims)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	path := "/"
	if bp := strings.TrimRight(h.S.Config.PanelBasePath, "/"); bp != "" && bp != "/" {
		path = bp
	}
	c.SetCookie(portalCookie, tok, int(portalTTL.Seconds()), path, "", secure, true)
	return tok
}

// Login with subscription token (Sub ID) — works without Telegram.
func (h *PortalHandler) Login(c *gin.Context) {
	var req struct {
		SubID string `json:"subId"`
		Token string `json:"token"` // one-time web token from bot
	}
	_ = c.ShouldBindJSON(&req)
	subID := strings.TrimSpace(req.SubID)
	if subID == "" && req.Token != "" {
		// exchange one-time token
		key := "portal_ot_" + strings.TrimSpace(req.Token)
		var st models.PanelSetting
		if h.S.DB.Where("key = ?", key).First(&st).Error != nil {
			c.JSON(401, gin.H{"success": false, "msg": "لینک نامعتبر یا منقضی شده"})
			return
		}
		_ = h.S.DB.Delete(&st).Error // one-time
		parts := strings.SplitN(st.Value, "|", 2)
		if len(parts) != 2 {
			c.JSON(401, gin.H{"success": false, "msg": "لینک خراب است"})
			return
		}
		exp, _ := strconv.ParseInt(parts[1], 10, 64)
		if exp < time.Now().Unix() {
			c.JSON(401, gin.H{"success": false, "msg": "لینک منقضی شده"})
			return
		}
		subID = parts[0]
	}
	if subID == "" || len(subID) < 8 || len(subID) > 64 {
		c.JSON(400, gin.H{"success": false, "msg": "Sub ID نامعتبر است"})
		return
	}
	var cl models.Client
	if h.S.DB.Where("sub_id = ?", subID).First(&cl).Error != nil {
		c.JSON(401, gin.H{"success": false, "msg": "اکانت پیدا نشد"})
		return
	}
	tok := h.issueSession(c, cl)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"token": tok, "subId": cl.SubID, "email": cl.Email, "name": cl.SubscriptionName,
	}})
}

func (h *PortalHandler) Logout(c *gin.Context) {
	path := "/"
	if bp := strings.TrimRight(h.S.Config.PanelBasePath, "/"); bp != "" && bp != "/" {
		path = bp
	}
	c.SetCookie(portalCookie, "", -1, path, "", false, true)
	c.JSON(200, gin.H{"success": true})
}

func (h *PortalHandler) Dashboard(c *gin.Context) {
	cl, ok := h.authClient(c)
	if !ok {
		return
	}
	var ib models.Inbound
	_ = h.S.DB.First(&ib, cl.InboundID)
	host := strings.TrimSpace(telegram.GetSetting(h.S.DB, "public_host"))
	if host == "" && h.S.Config != nil {
		host = h.S.Config.PublicHost
	}
	share, _ := xray.BuildShareLink(ib, cl, host)
	base := strings.TrimRight(telegram.GetSetting(h.S.DB, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(host, "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	subURL := ""
	if base != "" {
		subURL = strings.TrimRight(base, "/") + "/sub/" + cl.SubID
	}
	limit := cl.TotalGB * 1024 * 1024 * 1024
	if cl.TrafficLimitBytes > 0 {
		limit = cl.TrafficLimitBytes
	}
	used := cl.Up + cl.Down
	var tgID int64
	var link models.TelegramCustomer
	if h.S.DB.Where("client_id = ?", cl.ID).First(&link).Error == nil {
		tgID = link.TelegramUserID
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"email": cl.Email, "subId": cl.SubID, "name": cl.SubscriptionName,
		"enable": cl.Enable, "status": cl.SubscriptionStatus,
		"totalGB": cl.TotalGB, "usedBytes": used, "limitBytes": limit,
		"expiryTime": cl.ExpiryTime, "shareLink": share, "subUrl": subURL,
		"inbound": ib.Remark, "telegramLinked": tgID > 0,
	}})
}

func (h *PortalHandler) Plans(c *gin.Context) {
	if _, ok := h.authClient(c); !ok {
		return
	}
	var plans []models.ShopPlan
	h.S.DB.Where("enabled = ?", true).Order("sort_order asc, id asc").Find(&plans)
	card, holder, inst, enabled := shop.PaymentInfo(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"plans": plans, "shopEnabled": enabled,
		"cardNumber": card, "cardHolder": holder, "instructions": inst,
	}})
}

func (h *PortalHandler) RedeemGift(c *gin.Context) {
	cl, ok := h.authClient(c)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Code) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "code required"})
		return
	}
	// Find telegram id linked to this client, else use 0 and redeem onto same client path via synthetic
	var link models.TelegramCustomer
	tgID := int64(0)
	chatID := int64(0)
	if h.S.DB.Where("client_id = ?", cl.ID).First(&link).Error == nil {
		tgID = link.TelegramUserID
		chatID = link.TelegramChatID
	}
	if tgID == 0 {
		// Allow redeem by creating/updating this client only: use negative sentinel based on client id
		// Prefer requiring telegram — return clear message
		c.JSON(400, gin.H{"success": false, "msg": "برای فعال‌سازی هدیه، اکانت باید به تلگرام وصل باشد یا از ربات /redeem استفاده کنید"})
		return
	}
	newCl, gift, err := shop.RedeemGift(h.S.DB, req.Code, tgID, chatID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = h.S.RegenerateAndRestartXray()
	// re-issue session if sub id changed
	tok := h.issueSession(c, newCl)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"message": "هدیه فعال شد", "planName": gift.PlanName, "subId": newCl.SubID, "token": tok,
	}})
}
