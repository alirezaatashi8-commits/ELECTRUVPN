package handlers

import (
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/routing"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChainHandler struct{ S *core.Server }

func NewChainHandler(s *core.Server) *ChainHandler { return &ChainHandler{S: s} }

func (h *ChainHandler) List(c *gin.Context) {
	var rows []models.TunnelChain
	h.S.DB.Order("id asc").Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// HopCandidates returns panel nodes eligible as mid/exit hops (for admin UI).
func (h *ChainHandler) HopCandidates(c *gin.Context) {
	cands, err := routing.SelectNodes(h.S.DB, routing.SelectOptions{
		Strategy: "least_load", Failover: false, HealthFilter: "any", MaxNodes: 64,
	})
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	var nodes []models.ServerNode
	h.S.DB.Where("enabled = ?", true).Order("name asc").Find(&nodes)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"ranked": cands, "nodes": nodes}})
}

func (h *ChainHandler) Save(c *gin.Context) {
	var req models.TunnelChain
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "default-chain"
	}
	if req.SelectionMode == "" {
		req.SelectionMode = "manual"
	}
	if req.AutoStrategy == "" {
		req.AutoStrategy = "least_load"
	}
	// Fill address from node IDs when manual
	if req.ExitNodeID > 0 {
		var n models.ServerNode
		if h.S.DB.First(&n, req.ExitNodeID).Error == nil {
			if req.ExitAddress == "" {
				req.ExitAddress = n.Address
			}
			if req.ExitPort == 0 && n.Port > 0 {
				req.ExitPort = n.Port
			}
		}
	}
	if req.MidNodeID > 0 {
		var n models.ServerNode
		if h.S.DB.First(&n, req.MidNodeID).Error == nil {
			if req.MidAddress == "" {
				req.MidAddress = n.Address
			}
			if req.MidPort == 0 && n.Port > 0 {
				req.MidPort = n.Port
			}
		}
	}
	if req.Mid2NodeID > 0 {
		var n models.ServerNode
		if h.S.DB.First(&n, req.Mid2NodeID).Error == nil {
			if req.Mid2Address == "" {
				req.Mid2Address = n.Address
			}
			if req.Mid2Port == 0 && n.Port > 0 {
				req.Mid2Port = n.Port
			}
		}
	}
	req.ExitAddress = strings.TrimSpace(req.ExitAddress)
	req.MidAddress = strings.TrimSpace(req.MidAddress)
	if req.Enabled && req.SelectionMode != "auto" && (req.ExitAddress == "" || req.ExitPort <= 0) {
		c.JSON(400, gin.H{"success": false, "msg": "exit address and port required (or use auto + registered nodes)"})
		return
	}
	if req.ActiveExitHost == "" {
		req.ActiveExitHost = req.ExitAddress
	}
	if req.ActiveMidHost == "" {
		req.ActiveMidHost = req.MidAddress
	}
	req.Mid2Address = strings.TrimSpace(req.Mid2Address)
	if req.ActiveMid2Host == "" {
		req.ActiveMid2Host = req.Mid2Address
	}

	updates := map[string]any{
		"name": req.Name, "enabled": req.Enabled,
		"selection_mode": req.SelectionMode, "auto_strategy": req.AutoStrategy,
		"mid_node_id": req.MidNodeID, "exit_node_id": req.ExitNodeID,
		"mid_address": req.MidAddress, "mid_port": req.MidPort, "mid_protocol": req.MidProtocol,
		"mid_uuid": req.MidUUID, "mid_password": req.MidPassword, "mid_flow": req.MidFlow,
		"mid_sni": req.MidSNI, "mid_public_key": req.MidPublicKey, "mid_short_id": req.MidShortID,
		"mid_fingerprint": req.MidFingerprint, "mid_network": req.MidNetwork,
		"mid2_address": req.Mid2Address, "mid2_port": req.Mid2Port, "mid2_protocol": req.Mid2Protocol,
		"mid2_uuid": req.Mid2UUID, "mid2_password": req.Mid2Password, "mid2_flow": req.Mid2Flow,
		"mid2_sni": req.Mid2SNI, "mid2_public_key": req.Mid2PublicKey, "mid2_short_id": req.Mid2ShortID,
		"mid2_fingerprint": req.Mid2Fingerprint, "mid2_network": req.Mid2Network, "mid2_node_id": req.Mid2NodeID,
		"exit_address": req.ExitAddress, "exit_port": req.ExitPort, "exit_protocol": req.ExitProtocol,
		"exit_uuid": req.ExitUUID, "exit_password": req.ExitPassword, "exit_flow": req.ExitFlow,
		"exit_sni": req.ExitSNI, "exit_public_key": req.ExitPublicKey, "exit_short_id": req.ExitShortID,
		"exit_fingerprint": req.ExitFingerprint, "exit_network": req.ExitNetwork,
		"rotate_addresses": req.RotateAddresses, "rotate_on_fail": req.RotateOnFail,
		"confirm_before_failover": req.ConfirmBeforeFailover,
		"active_exit_host":        req.ActiveExitHost, "active_mid_host": req.ActiveMidHost, "active_mid2_host": req.ActiveMid2Host,
	}

	// The admin UI does not send per-hop flow values; do not wipe a stored flow
	// (e.g. xtls-rprx-vision) just because the request omitted it.
	if req.ID > 0 {
		for k, v := range map[string]string{"mid_flow": req.MidFlow, "mid2_flow": req.Mid2Flow, "exit_flow": req.ExitFlow} {
			if v == "" {
				delete(updates, k)
			}
		}
	}

	if req.ID > 0 {
		if err := h.S.DB.Model(&models.TunnelChain{}).Where("id = ?", req.ID).Updates(updates).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	} else {
		if err := h.S.DB.Create(&req).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	if req.Enabled {
		h.S.DB.Model(&models.TunnelChain{}).Where("id <> ? AND enabled = ?", req.ID, true).Update("enabled", false)
	}
	auditAction(h.S, c, "tunnel_chain.save", strconv.FormatUint(uint64(req.ID), 10), gin.H{
		"enabled": req.Enabled, "mode": req.SelectionMode, "exit": req.ExitAddress, "mid": req.MidAddress,
	})
	if rerr := h.S.RegenerateAndRestartXray(); rerr != nil {
		c.JSON(200, gin.H{"success": true, "obj": req, "warning": "saved, but Xray reload failed: " + rerr.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": req})
}

func (h *ChainHandler) SetEnabled(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Enabled bool `json:"enabled"`
	}
	_ = c.ShouldBindJSON(&req)
	var row models.TunnelChain
	if h.S.DB.First(&row, id).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	if req.Enabled && row.SelectionMode != "auto" && (row.ExitAddress == "" || row.ExitPort <= 0) {
		c.JSON(400, gin.H{"success": false, "msg": "configure exit before enabling"})
		return
	}
	_ = h.S.DB.Model(&row).Update("enabled", req.Enabled).Error
	if req.Enabled {
		h.S.DB.Model(&models.TunnelChain{}).Where("id <> ?", row.ID).Update("enabled", false)
	}
	auditAction(h.S, c, "tunnel_chain.toggle", c.Param("id"), gin.H{"enabled": req.Enabled})
	_ = h.S.RegenerateAndRestartXray()
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": row.ID, "enabled": req.Enabled}})
}

func (h *ChainHandler) Rotate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var row models.TunnelChain
	if h.S.DB.First(&row, id).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	next := xray.RotateChainHost(h.S.DB, &row)
	_ = h.S.RegenerateAndRestartXray()
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"activeExitHost": next}})
}

// Delete removes a chain definition.
func (h *ChainHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	_ = h.S.DB.Delete(&models.TunnelChain{}, id).Error
	auditAction(h.S, c, "tunnel_chain.delete", c.Param("id"), nil)
	_ = h.S.RegenerateAndRestartXray()
	c.JSON(200, gin.H{"success": true})
}

var _ = gorm.ErrRecordNotFound
