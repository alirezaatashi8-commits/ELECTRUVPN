package handlers

import (
	"strconv"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/obfuscation"
	"github.com/gin-gonic/gin"
)

type ObfuscationHandler struct{ S *core.Server }

func NewObfuscationHandler(s *core.Server) *ObfuscationHandler { return &ObfuscationHandler{S: s} }

func (h *ObfuscationHandler) ListDests(c *gin.Context) {
	obfuscation.SeedDefaults(h.S.DB)
	activeOnly := c.Query("active") != "0"
	rows := obfuscation.ListDests(h.S.DB, activeOnly)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

func (h *ObfuscationHandler) UpsertDest(c *gin.Context) {
	var req struct {
		Domain   string `json:"domain"`
		Provider string `json:"provider"`
		Notes    string `json:"notes"`
		Port     int    `json:"port"`
		Priority int    `json:"priority"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Domain == "" {
		c.JSON(400, gin.H{"success": false, "msg": "domain required"})
		return
	}
	row, err := obfuscation.UpsertDest(h.S.DB, req.Domain, req.Provider, req.Notes, req.Port, req.Priority)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "obfuscation.dest.upsert", row.Domain, row)
	c.JSON(200, gin.H{"success": true, "obj": row})
}

func (h *ObfuscationHandler) DeleteDest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "id required"})
		return
	}
	if err := h.S.DB.Delete(&models.RealityDestPool{}, id).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "obfuscation.dest.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

func (h *ObfuscationHandler) HealthCheck(c *gin.Context) {
	checked, healthy := obfuscation.HealthCheckAll(h.S.DB)
	auditAction(h.S, c, "obfuscation.health", "pool", gin.H{"checked": checked, "healthy": healthy})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"checked": checked, "healthy": healthy, "dests": obfuscation.ListDests(h.S.DB, true)}})
}

func (h *ObfuscationHandler) ApplyDest(c *gin.Context) {
	var req struct {
		InboundID         uint `json:"inboundId"`
		DestID            uint `json:"destId"`
		MergeHealthyNames bool `json:"mergeHealthyNames"`
	}
	if c.ShouldBindJSON(&req) != nil || req.InboundID == 0 || req.DestID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "inboundId and destId required"})
		return
	}
	ib, err := obfuscation.ApplyDestToInbound(h.S.DB, req.InboundID, req.DestID, req.MergeHealthyNames)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "dest saved but xray reload failed: " + err.Error()})
		return
	}
	auditAction(h.S, c, "obfuscation.apply_dest", strconv.FormatUint(uint64(req.InboundID), 10), gin.H{"destId": req.DestID, "merge": req.MergeHealthyNames})
	c.JSON(200, gin.H{"success": true, "obj": ib, "msg": "dest applied; private keys unchanged — clients get new SNI on next subscription update"})
}
