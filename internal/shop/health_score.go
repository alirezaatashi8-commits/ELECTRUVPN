package shop

import (
	"fmt"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
)

// HealthScore is a 0–100 subscription health indicator for operators and end users.
type HealthScore struct {
	Score      int            `json:"score"`
	Level      string         `json:"level"` // excellent | good | fair | poor | critical | none
	Label      string         `json:"label"`
	Color      string         `json:"color"` // css-ish hint: green | teal | amber | orange | red | gray
	Factors    []HealthFactor `json:"factors"`
	Summary    string         `json:"summary"`
	ComputedAt time.Time      `json:"computedAt"`
}

type HealthFactor struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Score  int    `json:"score"`  // 0–100 contribution before weight
	Weight int    `json:"weight"` // relative weight
	Detail string `json:"detail"`
}

// HealthInput carries optional runtime context beyond the Client row.
type HealthInput struct {
	ActiveDevices int
	DeviceLimit   int
	InboundEnable *bool
	Now           time.Time
}

// ComputeHealthScore rates subscription health from expiry, traffic, enablement,
// recent activity, device pressure, and optional inbound status.
func ComputeHealthScore(cl models.Client, in HealthInput) HealthScore {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	if cl.ID == 0 {
		return HealthScore{
			Score: 0, Level: "none", Label: "بدون اشتراک", Color: "gray",
			Summary: "هنوز اشتراکی فعال نیست.", ComputedAt: now,
			Factors: []HealthFactor{},
		}
	}

	factors := make([]HealthFactor, 0, 6)

	// 1) Account enabled (weight 15)
	enScore := 100
	enDetail := "اکانت فعال است"
	if !cl.Enable {
		enScore = 0
		enDetail = "اکانت غیرفعال است"
	}
	factors = append(factors, HealthFactor{Key: "enabled", Label: "وضعیت اکانت", Score: enScore, Weight: 15, Detail: enDetail})

	// 2) Time remaining (weight 30)
	timeScore := 100
	timeDetail := "بدون انقضا / نامحدود"
	if cl.ExpiryTime > 0 {
		msLeft := cl.ExpiryTime - now.UnixMilli()
		days := float64(msLeft) / 86400000.0
		if msLeft <= 0 {
			timeScore = 0
			timeDetail = "منقضی شده"
		} else if days >= 14 {
			timeScore = 100
			timeDetail = sprintfDays(days) + " باقی مانده"
		} else if days >= 7 {
			timeScore = 80
			timeDetail = sprintfDays(days) + " باقی مانده"
		} else if days >= 3 {
			timeScore = 55
			timeDetail = sprintfDays(days) + " باقی مانده — نزدیک انقضا"
		} else if days >= 1 {
			timeScore = 30
			timeDetail = sprintfDays(days) + " باقی مانده — فوری"
		} else {
			timeScore = 10
			timeDetail = "کمتر از یک روز تا انقضا"
		}
	}
	factors = append(factors, HealthFactor{Key: "expiry", Label: "زمان باقی‌مانده", Score: timeScore, Weight: 30, Detail: timeDetail})

	// 3) Traffic remaining (weight 30)
	used := cl.Up + cl.Down
	limit := cl.TrafficLimitBytes
	if limit <= 0 && cl.TotalGB > 0 {
		limit = cl.TotalGB * 1024 * 1024 * 1024
	}
	trScore := 100
	trDetail := "ترافیک نامحدود یا بدون سقف"
	if limit > 0 {
		rem := float64(limit-used) / float64(limit) * 100
		if rem < 0 {
			rem = 0
		}
		if used >= limit {
			trScore = 0
			trDetail = "ترافیک تمام شده"
		} else if rem >= 40 {
			trScore = 100
			trDetail = sprintfPct(rem) + " باقی مانده"
		} else if rem >= 20 {
			trScore = 70
			trDetail = sprintfPct(rem) + " باقی مانده"
		} else if rem >= 10 {
			trScore = 40
			trDetail = sprintfPct(rem) + " — روبه‌اتمام"
		} else {
			trScore = 15
			trDetail = sprintfPct(rem) + " — بحرانی"
		}
	}
	factors = append(factors, HealthFactor{Key: "traffic", Label: "ترافیک", Score: trScore, Weight: 30, Detail: trDetail})

	// 4) Activity / last seen (weight 15)
	actScore := 70 // neutral if unknown
	actDetail := "آخرین اتصال ثبت نشده"
	last := cl.SubscriptionLastSeenAt
	if last.IsZero() {
		// fallback: UpdatedAt is weak signal
		if !cl.UpdatedAt.IsZero() && now.Sub(cl.UpdatedAt) < 72*time.Hour {
			actScore = 60
			actDetail = "فعالیت اخیر نامشخص"
		}
	} else {
		age := now.Sub(last)
		if age <= 24*time.Hour {
			actScore = 100
			actDetail = "آخرین اتصال کمتر از ۲۴ ساعت پیش"
		} else if age <= 72*time.Hour {
			actScore = 75
			actDetail = "آخرین اتصال ۱–۳ روز پیش"
		} else if age <= 7*24*time.Hour {
			actScore = 45
			actDetail = "آخرین اتصال بیش از ۳ روز پیش"
		} else {
			actScore = 15
			actDetail = "مدت‌هاست اتصال دیده نشده"
		}
	}
	factors = append(factors, HealthFactor{Key: "activity", Label: "فعالیت اخیر", Score: actScore, Weight: 15, Detail: actDetail})

	// 5) Device pressure (weight 10)
	devScore := 100
	devDetail := "بدون محدودیت دستگاه"
	if in.DeviceLimit > 0 {
		if in.ActiveDevices >= in.DeviceLimit {
			devScore = 25
			devDetail = "ظرفیت دستگاه پر است"
		} else if float64(in.ActiveDevices) >= float64(in.DeviceLimit)*0.8 {
			devScore = 55
			devDetail = "نزدیک سقف دستگاه"
		} else {
			devScore = 100
			devDetail = "دستگاه در محدوده مجاز"
		}
	} else if in.ActiveDevices > 8 {
		devScore = 60
		devDetail = "تعداد دستگاه بالا بدون سقف"
	}
	factors = append(factors, HealthFactor{Key: "devices", Label: "دستگاه‌ها", Score: devScore, Weight: 10, Detail: devDetail})

	// 6) Inbound / node (weight 10) — optional
	if in.InboundEnable != nil {
		ibScore := 100
		ibDetail := "اینباند/نود فعال"
		if !*in.InboundEnable {
			ibScore = 0
			ibDetail = "اینباند غیرفعال است"
		}
		factors = append(factors, HealthFactor{Key: "inbound", Label: "مسیر اتصال", Score: ibScore, Weight: 10, Detail: ibDetail})
	}

	totalW := 0
	acc := 0
	for _, f := range factors {
		totalW += f.Weight
		acc += f.Score * f.Weight
	}
	score := 0
	if totalW > 0 {
		score = acc / totalW
	}
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	// Hard floors
	if !cl.Enable {
		if score > 20 {
			score = 20
		}
	}
	if cl.ExpiryTime > 0 && cl.ExpiryTime <= now.UnixMilli() {
		if score > 15 {
			score = 15
		}
	}
	if limit > 0 && used >= limit {
		if score > 20 {
			score = 20
		}
	}

	level, label, color := healthLevel(score)
	summary := buildHealthSummary(level, factors)

	return HealthScore{
		Score: score, Level: level, Label: label, Color: color,
		Factors: factors, Summary: summary, ComputedAt: now,
	}
}

func healthLevel(score int) (level, label, color string) {
	switch {
	case score >= 85:
		return "excellent", "عالی", "green"
	case score >= 70:
		return "good", "خوب", "teal"
	case score >= 50:
		return "fair", "متوسط", "amber"
	case score >= 30:
		return "poor", "ضعیف", "orange"
	default:
		return "critical", "بحرانی", "red"
	}
}

func buildHealthSummary(level string, factors []HealthFactor) string {
	// pick weakest non-perfect factor for a human line
	worst := HealthFactor{Score: 101}
	for _, f := range factors {
		if f.Score < worst.Score {
			worst = f
		}
	}
	switch level {
	case "excellent":
		return "اشتراک در وضعیت پایدار است."
	case "good":
		return "وضعیت کلی خوب است؛ " + worst.Detail
	case "fair":
		return "نیاز به توجه: " + worst.Detail
	case "poor":
		return "وضعیت نامناسب — " + worst.Detail
	default:
		return "وضعیت بحرانی — " + worst.Detail
	}
}

func sprintfDays(days float64) string {
	if days < 0 {
		days = 0
	}
	if days >= 10 {
		return fmt.Sprintf("%.0f روز", days)
	}
	return fmt.Sprintf("%.1f روز", days)
}

func sprintfPct(p float64) string {
	return fmt.Sprintf("%.0f%%", p)
}
