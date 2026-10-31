package handlers

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
)

// SpeedTestTargets returns public probe targets for client-side latency tests.
// Latency is measured in the Mini App on the user's device (true user-path RTT),
// not from the panel server. Server load is attached so the UI can rank by
// "fastest + least congested".
func (h *TMAHandler) SpeedTestTargets(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	_ = tgID

	type target struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		Host     string  `json:"host"`
		Port     int     `json:"port"`
		ProbeURL string  `json:"probeUrl"`
		LoadPct  float64 `json:"loadPct"`
		Healthy  bool    `json:"healthy"`
		Kind     string  `json:"kind"` // node | inbound
		Hint     string  `json:"hint,omitempty"`
	}

	publicHost := strings.TrimSpace(telegram.GetSetting(h.S.DB, "public_host"))
	if publicHost == "" && h.S.Config != nil {
		publicHost = strings.TrimSpace(h.S.Config.PublicHost)
		if publicHost == "" {
			publicHost = strings.TrimSpace(h.S.Config.PublicHostDomain)
		}
		if publicHost == "" {
			publicHost = strings.TrimSpace(h.S.Config.PublicHostV4)
		}
	}
	publicHost = stripHostScheme(publicHost)

	seen := map[string]bool{}
	var out []target

	// Registered panel nodes
	var nodes []models.ServerNode
	h.S.DB.Where("enabled = ?", true).Order("id asc").Find(&nodes)
	var agents []models.AgentNode
	h.S.DB.Find(&agents)
	agentBy := map[uint]models.AgentNode{}
	for _, a := range agents {
		agentBy[a.NodeID] = a
	}
	for _, n := range nodes {
		host := strings.TrimSpace(n.Address)
		if host == "" {
			continue
		}
		host = stripHostScheme(host)
		port := n.Port
		if port <= 0 {
			port = 443
		}
		key := strings.ToLower(host) + ":" + strconv.Itoa(port)
		if seen[key] {
			continue
		}
		seen[key] = true
		load := 0.0
		healthy := true
		if a, ok := agentBy[n.ID]; ok {
			load = a.CPUPercent
			if a.MemoryTotalBytes > 0 {
				memPct := 100 * float64(a.MemoryBytes) / float64(a.MemoryTotalBytes)
				if memPct > load {
					load = memPct
				}
			}
			healthy = a.HealthState == "healthy" || a.HealthState == "" || a.HealthState == "unknown"
			if a.LastError != "" {
				healthy = false
			}
			if a.LastSeenAt != nil && time.Since(*a.LastSeenAt) > 2*time.Minute {
				healthy = false
			}
		}
		probe := "https://" + host + "/"
		if port != 443 && port != 80 {
			probe = fmt.Sprintf("https://%s:%d/", host, port)
		}
		out = append(out, target{
			ID: fmt.Sprintf("node-%d", n.ID), Name: n.Name, Host: host, Port: port,
			ProbeURL: probe, LoadPct: load, Healthy: healthy, Kind: "node",
		})
	}

	// Enabled inbounds (common single-server deploy)
	var inbounds []models.Inbound
	h.S.DB.Where("enable = ?", true).Order("id asc").Limit(12).Find(&inbounds)
	for _, ib := range inbounds {
		host := publicHost
		if host == "" {
			continue
		}
		port := ib.Port
		if port <= 0 {
			port = 443
		}
		key := strings.ToLower(host) + ":" + strconv.Itoa(port)
		if seen[key] {
			continue
		}
		seen[key] = true
		name := ib.Remark
		if name == "" {
			name = fmt.Sprintf("Inbound %d", ib.ID)
		}
		probe := "https://" + host + "/"
		if port == 80 {
			probe = "http://" + host + "/"
		} else if port != 443 {
			probe = fmt.Sprintf("https://%s:%d/", host, port)
		}
		out = append(out, target{
			ID: fmt.Sprintf("ib-%d", ib.ID), Name: name, Host: host, Port: port,
			ProbeURL: probe, LoadPct: 0, Healthy: true, Kind: "inbound",
			Hint: "پورت سرویس شما",
		})
	}

	// Fallback: public host alone
	if len(out) == 0 && publicHost != "" {
		out = append(out, target{
			ID: "pub", Name: "سرور اصلی", Host: publicHost, Port: 443,
			ProbeURL: "https://" + publicHost + "/", Healthy: true, Kind: "inbound",
		})
	}

	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"targets": out,
		"method":  "client-side",
		"note":    "پینگ از دستگاه شما اندازه‌گیری می‌شود (مسیر واقعی اینترنت شما).",
	}})
}

// SpeedTestServerPing runs short TCP connects from the panel to each target.
// Useful as a secondary signal (server path), not a replacement for client ping.
func (h *TMAHandler) SpeedTestServerPing(c *gin.Context) {
	if _, ok := h.authUser(c); !ok {
		return
	}
	var req struct {
		Targets []struct {
			ID   string `json:"id"`
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"targets"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Targets) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "targets required"})
		return
	}
	if len(req.Targets) > 20 {
		req.Targets = req.Targets[:20]
	}

	type result struct {
		ID        string `json:"id"`
		LatencyMs int64  `json:"latencyMs"`
		OK        bool   `json:"ok"`
		Error     string `json:"error,omitempty"`
	}
	results := make([]result, len(req.Targets))
	var wg sync.WaitGroup
	for i, t := range req.Targets {
		wg.Add(1)
		go func(i int, host string, port int, id string) {
			defer wg.Done()
			host = stripHostScheme(strings.TrimSpace(host))
			if port <= 0 {
				port = 443
			}
			if host == "" {
				results[i] = result{ID: id, OK: false, Error: "empty host", LatencyMs: -1}
				return
			}
			addr := net.JoinHostPort(host, strconv.Itoa(port))
			ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
			defer cancel()
			start := time.Now()
			d := net.Dialer{Timeout: 2 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", addr)
			ms := time.Since(start).Milliseconds()
			if err != nil {
				results[i] = result{ID: id, OK: false, Error: "timeout/unreachable", LatencyMs: ms}
				return
			}
			_ = conn.Close()
			results[i] = result{ID: id, OK: true, LatencyMs: ms}
		}(i, t.Host, t.Port, t.ID)
	}
	wg.Wait()
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"results": results, "method": "server-tcp"}})
}

// UsageHistory returns advanced traffic analytics for the authenticated TMA user.
// Query params:
//
//	range = day | week | month   (default week)
//	days  = 1..90               (overrides range length when set)
//
// Response includes:
//   - points: time-bucketed usage deltas (raw measured bytes)
//   - dayNight: aggregate day (06-18 UTC) vs night (18-06 UTC) usage
//   - summary: total/avg/peak over the window
func (h *TMAHandler) UsageHistory(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"points": []any{}, "hasClient": false}})
		return
	}

	rangeKey := strings.ToLower(strings.TrimSpace(c.DefaultQuery("range", "week")))
	days, _ := strconv.Atoi(c.DefaultQuery("days", "0"))
	switch {
	case days > 0:
		// explicit
	case rangeKey == "day":
		days = 1
	case rangeKey == "month":
		days = 30
	default:
		rangeKey = "week"
		days = 7
	}
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}

	from := time.Now().UTC().AddDate(0, 0, -days).Truncate(24 * time.Hour)
	var snaps []models.UsageSnapshot
	h.S.DB.Where("client_id = ? AND created_at >= ?", cl.ID, from).
		Order("created_at asc").Find(&snaps)

	type point struct {
		Date    string  `json:"date"`
		Label   string  `json:"label"`
		Up      int64   `json:"up"`
		Down    int64   `json:"down"`
		Total   int64   `json:"total"`
		TotalGB float64 `json:"totalGB"`
		Hour    int     `json:"hour,omitempty"` // for day range hourly buckets
	}

	type cum struct{ up, down int64 }

	// ---- Daily series (always) ----
	byDay := map[string]cum{}
	order := make([]string, 0, days+1)
	for i := 0; i <= days; i++ {
		d := from.AddDate(0, 0, i).Format("2006-01-02")
		order = append(order, d)
		byDay[d] = cum{}
	}
	for _, s := range snaps {
		d := s.CreatedAt.UTC().Format("2006-01-02")
		cur := byDay[d]
		if s.Up > cur.up {
			cur.up = s.Up
		}
		if s.Down > cur.down {
			cur.down = s.Down
		}
		byDay[d] = cur
	}
	var prev cum
	for _, d := range order {
		cur := byDay[d]
		if cur.up == 0 && cur.down == 0 && (prev.up > 0 || prev.down > 0) {
			byDay[d] = prev
			continue
		}
		if cur.up < prev.up {
			cur.up = prev.up
		}
		if cur.down < prev.down {
			cur.down = prev.down
		}
		byDay[d] = cur
		prev = cur
	}
	if len(snaps) == 0 {
		today := time.Now().UTC().Format("2006-01-02")
		byDay[today] = cum{up: cl.Up, down: cl.Down}
	}

	faShort := []string{"ی", "د", "س", "چ", "پ", "ج", "ش"}
	points := make([]point, 0, days)
	var prevTot int64
	for i, d := range order {
		c0 := byDay[d]
		tot := c0.up + c0.down
		if i == 0 {
			prevTot = tot
			continue
		}
		delta := tot - prevTot
		if delta < 0 {
			delta = 0
		}
		prevC := byDay[order[i-1]]
		upDelta := c0.up - prevC.up
		if upDelta < 0 {
			upDelta = 0
		}
		downDelta := delta - upDelta
		if downDelta < 0 {
			downDelta = 0
			upDelta = delta
		}
		t, _ := time.Parse("2006-01-02", d)
		label := faShort[int(t.Weekday())]
		if rangeKey == "month" {
			label = t.Format("01/02")
		}
		points = append(points, point{
			Date: d, Label: label,
			Up: upDelta, Down: downDelta, Total: delta,
			TotalGB: float64(delta) / (1024 * 1024 * 1024),
		})
		prevTot = tot
	}

	// ---- Hourly series for range=day (last 24h) ----
	hourly := []point{}
	if rangeKey == "day" {
		fromH := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Hour)
		byHour := map[string]cum{}
		hOrder := make([]string, 0, 25)
		for i := 0; i <= 24; i++ {
			k := fromH.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15")
			hOrder = append(hOrder, k)
			byHour[k] = cum{}
		}
		for _, s := range snaps {
			if s.CreatedAt.Before(fromH) {
				continue
			}
			k := s.CreatedAt.UTC().Format("2006-01-02T15")
			cur := byHour[k]
			if s.Up > cur.up {
				cur.up = s.Up
			}
			if s.Down > cur.down {
				cur.down = s.Down
			}
			byHour[k] = cur
		}
		var pH cum
		var prevHTot int64
		for i, k := range hOrder {
			cur := byHour[k]
			if cur.up == 0 && cur.down == 0 && (pH.up > 0 || pH.down > 0) {
				byHour[k] = pH
				cur = pH
			} else {
				if cur.up < pH.up {
					cur.up = pH.up
				}
				if cur.down < pH.down {
					cur.down = pH.down
				}
				byHour[k] = cur
				pH = cur
			}
			tot := cur.up + cur.down
			if i == 0 {
				prevHTot = tot
				continue
			}
			delta := tot - prevHTot
			if delta < 0 {
				delta = 0
			}
			hour := 0
			if t, err := time.Parse("2006-01-02T15", k); err == nil {
				hour = t.Hour()
			}
			hourly = append(hourly, point{
				Date: k, Label: fmt.Sprintf("%02d", hour), Hour: hour,
				Total: delta, TotalGB: float64(delta) / (1024 * 1024 * 1024),
			})
			prevHTot = tot
		}
		if len(hourly) > 0 {
			points = hourly
		}
	}

	// ---- Day vs Night (UTC): day 06-18, night 18-06 ----
	var dayBytes, nightBytes int64
	if len(snaps) >= 2 {
		for i := 1; i < len(snaps); i++ {
			prevS, curS := snaps[i-1], snaps[i]
			dUp := curS.Up - prevS.Up
			dDown := curS.Down - prevS.Down
			if dUp < 0 {
				dUp = 0
			}
			if dDown < 0 {
				dDown = 0
			}
			delta := dUp + dDown
			h := curS.CreatedAt.UTC().Hour()
			if h >= 6 && h < 18 {
				dayBytes += delta
			} else {
				nightBytes += delta
			}
		}
	}

	var peak, sum int64
	for _, p := range points {
		sum += p.Total
		if p.Total > peak {
			peak = p.Total
		}
	}
	avg := int64(0)
	if len(points) > 0 {
		avg = sum / int64(len(points))
	}

	var dayPct, nightPct float64
	if dn := dayBytes + nightBytes; dn > 0 {
		dayPct = float64(dayBytes) * 100 / float64(dn)
		nightPct = float64(nightBytes) * 100 / float64(dn)
	}

	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"hasClient":   true,
		"range":       rangeKey,
		"days":        days,
		"points":      points,
		"peakBytes":   peak,
		"totalBytes":  sum,
		"avgBytes":    avg,
		"clientId":    cl.ID,
		"usedBytes":   cl.Up + cl.Down,
		"sampleCount": len(snaps),
		"dayNight": gin.H{
			"dayBytes":   dayBytes,
			"nightBytes": nightBytes,
			"dayPct":     dayPct,
			"nightPct":   nightPct,
		},
	}})
}

func stripHostScheme(h string) string {
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	if i := strings.Index(h, "/"); i >= 0 {
		h = h[:i]
	}
	// strip port from host if accidentally included for display host field callers
	if i := strings.Index(h, ":"); i >= 0 {
		// keep IPv6
		if !strings.Contains(h, "]") && net.ParseIP(h) == nil {
			// might be host:port
			if _, err := strconv.Atoi(h[i+1:]); err == nil {
				h = h[:i]
			}
		}
	}
	return h
}

// RankTargets is a pure helper for tests / optional server ranking.
func RankTargetsByScore(latencyMs map[string]int64, loadPct map[string]float64, ids []string) []string {
	type sc struct {
		id    string
		score float64
	}
	var list []sc
	for _, id := range ids {
		lat := latencyMs[id]
		if lat <= 0 {
			lat = 9999
		}
		load := loadPct[id]
		if load < 0 {
			load = 0
		}
		// score: lower is better — latency weighted by congestion
		score := float64(lat) * (1.0 + load/100.0)
		list = append(list, sc{id: id, score: score})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score < list[j].score })
	out := make([]string, len(list))
	for i := range list {
		out[i] = list[i].id
	}
	return out
}

// ReferralInfo returns the user's invite link and stats.
func (h *TMAHandler) ReferralInfo(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cfg := shop.LoadReferralConfig(h.S.DB)
	st := shop.GetReferralStats(h.S.DB, tgID)
	link := ""
	if cfg.Enabled {
		link = shop.BuildReferralLink(h.S.DB, tgID)
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"enabled": cfg.Enabled, "percent": cfg.Percent,
		"firstPurchaseOnly": cfg.FirstPurchaseOnly,
		"link":              link, "code": shop.ReferralCodeForUser(tgID),
		"stats": st,
	}})
}

// ClaimTrial creates a one-click free trial for this Telegram user.
func (h *TMAHandler) ClaimTrial(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	cfg := shop.LoadTrialConfig(h.S.DB)
	meta := shop.TrialClaimMeta{
		ClientIP: c.ClientIP(),
		DeviceFP: strings.TrimSpace(c.GetHeader("X-Device-Fingerprint")),
	}
	if meta.DeviceFP == "" {
		// body optional fingerprint
		var body struct {
			DeviceFP string `json:"deviceFp"`
		}
		_ = c.ShouldBindJSON(&body)
		meta.DeviceFP = strings.TrimSpace(body.DeviceFP)
	}
	cl, err := shop.CreateTrialAccount(h.S.DB, tgID, tgID, cfg, meta)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		// trial created but xray may lag one cycle
	}
	host := strings.TrimSpace(telegram.GetSetting(h.S.DB, "public_host"))
	if host == "" && h.S.Config != nil {
		host = h.S.Config.PublicHost
	}
	var ib models.Inbound
	_ = h.S.DB.First(&ib, cl.InboundID)
	share, _ := xray.BuildShareLink(ib, cl, host)
	AuditTMA(h.S.DB, tgID, "tma.trial.claim", cl.SubID, gin.H{"mb": cfg.MB, "hours": cfg.Hours})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"subId": cl.SubID, "email": cl.Email, "shareLink": share,
		"mb": cfg.MB, "hours": cfg.Hours,
		"expiryTime": cl.ExpiryTime,
		"message":    "تست رایگان فعال شد — هر اکانت تلگرام فقط یک‌بار",
	}})
}

// SpeedTestPayload serves a fixed-size binary blob so the Mini App can measure
// download throughput from the user's network path to the panel (approx Mbps).
// Auth required to prevent anonymous bandwidth abuse.
func (h *TMAHandler) SpeedTestPayload(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	_ = tgID
	// 256 KiB — enough for a short sample without flooding mobile data
	const size = 256 * 1024
	buf := make([]byte, size)
	// deterministic pattern (not random — cheaper CPU)
	for i := range buf {
		buf[i] = byte(i)
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Payload-Bytes", strconv.Itoa(size))
	c.Data(200, "application/octet-stream", buf)
}

// GiftList returns buyable plans + user's gift cards.
func (h *TMAHandler) GiftList(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var plans []models.ShopPlan
	h.S.DB.Where("enabled = ?", true).Order("sort_order asc, id asc").Find(&plans)
	bought, redeemed := shop.ListUserGifts(h.S.DB, tgID)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"plans": plans, "bought": bought, "redeemed": redeemed,
		"loyaltyPct": shop.LoyaltyDiscountPct(h.S.DB),
	}})
}

// GiftBuy creates a pending gift order (card-to-card).
func (h *TMAHandler) GiftBuy(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var req struct {
		PlanID uint `json:"planId"`
	}
	if c.ShouldBindJSON(&req) != nil || req.PlanID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "planId required"})
		return
	}
	var plan models.ShopPlan
	if h.S.DB.First(&plan, req.PlanID).Error != nil || !plan.Enabled {
		c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
		return
	}
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status IN ?", tgID, []string{"pending_payment", "pending_review"}).
		Update("status", "cancelled").Error
	order := models.ShopOrder{
		TelegramUserID: tgID, TelegramChatID: tgID,
		PlanID: plan.ID, PlanName: "هدیه: " + plan.Name, TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: plan.PriceToman,
		InboundID: plan.InboundID, Status: "pending_payment", Kind: "gift",
	}
	if err := h.S.DB.Create(&order).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	card, holder, inst, _ := shop.PaymentInfo(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"orderId": order.ID, "planName": plan.Name, "priceToman": plan.PriceToman,
		"cardNumber": card, "cardHolder": holder, "instructions": inst,
		"hint": "پس از واریز رسید را در ربات بفرستید؛ کد هدیه بعد از تأیید ادمین می‌آید",
	}})
}

// GiftRedeem activates a gift code for the current Telegram user.
func (h *TMAHandler) GiftRedeem(c *gin.Context) {
	tgID, ok := h.authUser(c)
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
	cl, gift, err := shop.RedeemGift(h.S.DB, req.Code, tgID, tgID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = h.S.RegenerateAndRestartXray()
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"subId": cl.SubID, "planName": gift.PlanName, "totalGB": gift.TotalGB,
		"durationDays": gift.DurationDays, "code": gift.Code,
		"message": "کارت هدیه فعال شد",
	}})
}
