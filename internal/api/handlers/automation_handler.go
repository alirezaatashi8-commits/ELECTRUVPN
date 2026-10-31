package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/automation"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

type AutomationHandler struct{ S *core.Server }

// NewAutomationHandler creates or exposes the NewAutomationHandler handler.
func NewAutomationHandler(s *core.Server) *AutomationHandler { return &AutomationHandler{S: s} }

// Rules handles the Rules API operation.
func (h *AutomationHandler) Rules(c *gin.Context) {
	var rows []models.AutomationRule
	if err := h.S.DB.Order("id desc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateRule handles the CreateRule API operation.
func (h *AutomationHandler) CreateRule(c *gin.Context) {
	var r models.AutomationRule
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Event) == "" || strings.TrimSpace(r.Action) == "" || len(r.Name) > 128 || len(r.Condition) > 8192 || len(r.Payload) > 16384 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid automation rule"})
		return
	}
	if r.CooldownSec < 1 {
		r.CooldownSec = 300
	}
	if r.CooldownSec > 7*24*3600 {
		c.JSON(400, gin.H{"success": false, "msg": "cooldown is too large"})
		return
	}
	if !validRuleEvent(r.Event) || !validRuleAction(r.Action) {
		c.JSON(400, gin.H{"success": false, "msg": "unsupported automation event or action"})
		return
	}
	if r.Condition != "" {
		var v any
		if json.Unmarshal([]byte(r.Condition), &v) != nil {
			c.JSON(400, gin.H{"success": false, "msg": "condition must be valid JSON"})
			return
		}
	}
	if r.Payload != "" {
		var v any
		if json.Unmarshal([]byte(r.Payload), &v) != nil {
			c.JSON(400, gin.H{"success": false, "msg": "payload must be valid JSON"})
			return
		}
	}
	if err := h.S.DB.Create(&r).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "automation.rule.create", strconv.FormatUint(uint64(r.ID), 10), r.Name)
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// UpdateRule handles the UpdateRule API operation.
func (h *AutomationHandler) UpdateRule(c *gin.Context) {
	var r models.AutomationRule
	if h.S.DB.First(&r, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "rule not found"})
		return
	}
	var req models.AutomationRule
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid rule"})
		return
	}
	if len(req.Name) > 128 || len(req.Condition) > 8192 || len(req.Payload) > 16384 || !validRuleEvent(strings.TrimSpace(req.Event)) || !validRuleAction(strings.TrimSpace(req.Action)) || req.CooldownSec < 1 || req.CooldownSec > 7*24*3600 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid automation rule"})
		return
	}
	if req.Condition != "" {
		var v any
		if json.Unmarshal([]byte(req.Condition), &v) != nil {
			c.JSON(400, gin.H{"success": false, "msg": "condition must be valid JSON"})
			return
		}
	}
	if req.Payload != "" {
		var v any
		if json.Unmarshal([]byte(req.Payload), &v) != nil {
			c.JSON(400, gin.H{"success": false, "msg": "payload must be valid JSON"})
			return
		}
	}
	updates := map[string]any{"name": strings.TrimSpace(req.Name), "enabled": req.Enabled, "event": strings.TrimSpace(req.Event), "condition": req.Condition, "action": strings.TrimSpace(req.Action), "payload": req.Payload, "cooldown_sec": req.CooldownSec}
	if updates["name"] == "" || updates["event"] == "" || updates["action"] == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name, event and action are required"})
		return
	}
	if err := h.S.DB.Model(&r).Updates(updates).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.S.DB.First(&r, r.ID)
	auditAction(h.S, c, "automation.rule.update", strconv.FormatUint(uint64(r.ID), 10), r.Name)
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeleteRule handles the DeleteRule API operation.
func (h *AutomationHandler) DeleteRule(c *gin.Context) {
	if err := h.S.DB.Delete(&models.AutomationRule{}, c.Param("id")).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "automation.rule.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// Tasks handles the Tasks API operation.
func (h *AutomationHandler) Tasks(c *gin.Context) {
	var rows []models.ScheduledTask
	if err := h.S.DB.Order("id desc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateTask handles the CreateTask API operation.
func (h *AutomationHandler) CreateTask(c *gin.Context) {
	var r models.ScheduledTask
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Schedule) == "" || strings.TrimSpace(r.Kind) == "" || len(r.Name) > 128 || len(r.Payload) > 16384 || !validSchedule(r.Schedule) || !validTaskKind(r.Kind) {
		c.JSON(400, gin.H{"success": false, "msg": "invalid scheduled task"})
		return
	}
	if r.Payload != "" {
		var v any
		if json.Unmarshal([]byte(r.Payload), &v) != nil {
			c.JSON(400, gin.H{"success": false, "msg": "payload must be valid JSON"})
			return
		}
	}
	r.NextRunAt = nextTaskTime(r.Schedule, time.Now())
	if err := h.S.DB.Create(&r).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "scheduler.task.create", strconv.FormatUint(uint64(r.ID), 10), r.Name)
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// UpdateTask handles the UpdateTask API operation.
func (h *AutomationHandler) UpdateTask(c *gin.Context) {
	var r models.ScheduledTask
	if h.S.DB.First(&r, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "task not found"})
		return
	}
	var req models.ScheduledTask
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid task"})
		return
	}
	r.Name = strings.TrimSpace(req.Name)
	r.Enabled = req.Enabled
	r.Schedule = strings.TrimSpace(req.Schedule)
	r.Kind = strings.TrimSpace(req.Kind)
	r.Payload = req.Payload
	if r.Name == "" || r.Schedule == "" || r.Kind == "" || len(r.Name) > 128 || len(r.Payload) > 16384 || !validSchedule(r.Schedule) || !validTaskKind(r.Kind) {
		c.JSON(400, gin.H{"success": false, "msg": "name, schedule and kind are required"})
		return
	}
	if r.Payload != "" {
		var v any
		if json.Unmarshal([]byte(r.Payload), &v) != nil {
			c.JSON(400, gin.H{"success": false, "msg": "payload must be valid JSON"})
			return
		}
	}
	r.NextRunAt = nextTaskTime(r.Schedule, time.Now())
	if err := h.S.DB.Save(&r).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeleteTask handles the DeleteTask API operation.
func (h *AutomationHandler) DeleteTask(c *gin.Context) {
	if err := h.S.DB.Delete(&models.ScheduledTask{}, c.Param("id")).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "scheduler.task.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// RunNow handles the RunNow API operation.
func (h *AutomationHandler) RunNow(c *gin.Context) {
	automation.TickContext(c.Request.Context(), h.S.DB)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "scheduler tick executed"})
}

func validTaskKind(v string) bool {
	switch strings.TrimSpace(v) {
	case "snapshot_usage", "cleanup_sessions":
		return true
	}
	return false
}
func validRuleEvent(v string) bool {
	switch strings.TrimSpace(v) {
	case "node_health", "client_expiry", "certificate_expiry", "always":
		return true
	}
	return false
}
func validRuleAction(v string) bool {
	switch strings.TrimSpace(v) {
	case "notify_telegram", "refresh_alerts", "cleanup_sessions", "snapshot_usage":
		return true
	}
	return false
}
func validSchedule(v string) bool {
	v = strings.TrimSpace(v)
	if v == "@hourly" || v == "@daily" {
		return true
	}
	if strings.HasPrefix(v, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(v, "@every ")))
		return err == nil && d >= time.Minute && d <= 7*24*time.Hour
	}
	return false
}

func nextTaskTime(s string, now time.Time) *time.Time {
	if strings.HasPrefix(s, "@every ") {
		if d, e := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(s, "@every "))); e == nil && d > 0 {
			n := now.Add(d)
			return &n
		}
	}
	if s == "@hourly" {
		n := now.Add(time.Hour)
		return &n
	}
	if s == "@daily" {
		n := now.Add(24 * time.Hour)
		return &n
	}
	return nil
}
