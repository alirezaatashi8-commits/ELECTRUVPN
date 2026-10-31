package core

import (
	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// syncInboundLimitStates recomputes, for every inbound, whether its own volume
// (Inbound.TotalGB vs the summed client traffic) or its own expiry
// (Inbound.ExpiryTime) has been reached, and persists the cached LimitReached
// flag. It returns how many inbounds changed state; the caller must regenerate
// the runtime config when the result is > 0 so the listener is removed (or put
// back) exactly once per transition instead of on every sync pass.
//
// Usage is plain bytes, counted 1:1.
func syncInboundLimitStates(tx *gorm.DB) (int, error) {
	var rows []models.Inbound
	if err := tx.Select("id,enable,total_gb,expiry_time,up,down,limit_reached").Find(&rows).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, ib := range rows {
		limited := ib.IsLimited()
		if limited == ib.LimitReached {
			continue
		}
		if err := tx.Model(&models.Inbound{}).Where("id = ?", ib.ID).UpdateColumn("limit_reached", limited).Error; err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}
