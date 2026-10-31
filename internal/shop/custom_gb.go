package shop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

type CustomGBConfig struct {
	Enabled    bool  `json:"enabled"`
	PricePerGB int64 `json:"pricePerGB"` // toman
	MinGB      int   `json:"minGB"`
	MaxGB      int   `json:"maxGB"`
	Days       int   `json:"days"`
	InboundID  uint  `json:"inboundId"`
}

func LoadCustomGBConfig(db *gorm.DB) CustomGBConfig {
	en := panelSetting(db, models.CustomGBEnabledKey)
	price, _ := strconv.ParseInt(panelSetting(db, models.CustomGBPriceKey), 10, 64)
	minGB, _ := strconv.Atoi(panelSetting(db, models.CustomGBMinKey))
	maxGB, _ := strconv.Atoi(panelSetting(db, models.CustomGBMaxKey))
	days, _ := strconv.Atoi(panelSetting(db, models.CustomGBDaysKey))
	ib, _ := strconv.ParseUint(panelSetting(db, models.CustomGBInboundKey), 10, 64)
	if minGB <= 0 {
		minGB = 5
	}
	if maxGB <= 0 {
		maxGB = 500
	}
	if maxGB < minGB {
		maxGB = minGB
	}
	if days <= 0 {
		days = 30
	}
	if price < 0 {
		price = 0
	}
	return CustomGBConfig{
		Enabled:    en == "1" || strings.EqualFold(en, "true"),
		PricePerGB: price, MinGB: minGB, MaxGB: maxGB, Days: days, InboundID: uint(ib),
	}
}

func CalcCustomGBPrice(cfg CustomGBConfig, gb int) (int64, error) {
	if !cfg.Enabled {
		return 0, fmt.Errorf("خرید حجم دلخواه غیرفعال است")
	}
	if cfg.PricePerGB <= 0 {
		return 0, fmt.Errorf("قیمت هر گیگ توسط ادمین تنظیم نشده")
	}
	if gb < cfg.MinGB || gb > cfg.MaxGB {
		return 0, fmt.Errorf("حجم باید بین %d و %d گیگ باشد", cfg.MinGB, cfg.MaxGB)
	}
	return cfg.PricePerGB * int64(gb), nil
}

// ResolveCustomInbound picks inbound for custom volume orders.
func ResolveCustomInbound(db *gorm.DB, cfg CustomGBConfig) (uint, error) {
	if cfg.InboundID > 0 {
		var ib models.Inbound
		if db.First(&ib, cfg.InboundID).Error == nil && ib.Enable && !ib.IsLimited() {
			return ib.ID, nil
		}
	}
	var ib models.Inbound
	if err := db.Where("enable = ?", true).Order("id asc").First(&ib).Error; err != nil {
		return 0, fmt.Errorf("اینباند فعالی نیست")
	}
	return ib.ID, nil
}
