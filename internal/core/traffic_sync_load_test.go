package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/xray"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func loadTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:load_%s?mode=memory&cache=shared&_busy_timeout=5000", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Client{}, &models.Inbound{}, &models.UsageSnapshot{},
		&models.ClientConfigProfile{}, &models.PanelSetting{},
		&models.ShopOrder{}, &models.Wallet{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedClients(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	ib := models.Inbound{Remark: "lb", Port: 443, Protocol: models.ProtocolVLESS, Enable: true}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	batch := make([]models.Client, 0, n)
	for i := 0; i < n; i++ {
		batch = append(batch, models.Client{
			InboundID:          ib.ID,
			Email:              fmt.Sprintf("user%d@load.test", i),
			UUID:               fmt.Sprintf("uuid-%d", i),
			Enable:             true,
			TotalGB:            50,
			ExpiryTime:         time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
			Up:                 int64(i * 1000),
			Down:               int64(i * 500),
			SubID:              fmt.Sprintf("sub%08d", i),
			SubscriptionStatus: "active",
		})
	}
	if err := db.CreateInBatches(batch, 100).Error; err != nil {
		t.Fatal(err)
	}
}

func buildStats(n int, mul int64) map[string]*xray.Traffic {
	stats := make(map[string]*xray.Traffic, n)
	for i := 0; i < n; i++ {
		stats[fmt.Sprintf("user%d@load.test", i)] = &xray.Traffic{
			Uplink:   int64(i*1000)*mul + 10_000,
			Downlink: int64(i*500)*mul + 5_000,
		}
	}
	return stats
}

// TestTrafficSync_LoadManyClients verifies a single pass over hundreds of clients.
func TestTrafficSync_LoadManyClients(t *testing.T) {
	const n = 400
	db := loadTestDB(t)
	seedClients(t, db, n)
	s := &Server{DB: db, Config: &config.Config{}}
	start := time.Now()
	res := s.SyncTrafficWithStats(buildStats(n, 1), false)
	elapsed := time.Since(start)
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	if res.ClientsTouched == 0 {
		t.Fatal("expected traffic updates")
	}
	t.Logf("single pass n=%d touched=%d elapsed=%s", n, res.ClientsTouched, elapsed)
	if elapsed > 5*time.Second {
		t.Fatalf("too slow: %s", elapsed)
	}
}

// TestTrafficSync_ConcurrentPasses races multiple sync workers (simulates overlapping ticks).
func TestTrafficSync_ConcurrentPasses(t *testing.T) {
	const n = 200
	const workers = 8
	db := loadTestDB(t)
	seedClients(t, db, n)
	s := &Server{DB: db, Config: &config.Config{}}
	stats := buildStats(n, 2)

	var wg sync.WaitGroup
	var fails atomic.Int32
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := s.SyncTrafficWithStats(stats, false)
			if res.Error != "" {
				fails.Add(1)
				t.Log(res.Error)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	if fails.Load() > 0 {
		t.Fatalf("%d workers failed", fails.Load())
	}
	var count int64
	db.Model(&models.Client{}).Count(&count)
	if count != n {
		t.Fatalf("client count corrupted: %d", count)
	}
	t.Logf("concurrent workers=%d n=%d elapsed=%s", workers, n, elapsed)
}

// TestTrafficSync_DisableOverQuotaUnderLoad ensures quota disable still works at scale.
func TestTrafficSync_DisableOverQuotaUnderLoad(t *testing.T) {
	db := loadTestDB(t)
	ib := models.Inbound{Remark: "q", Port: 443, Protocol: models.ProtocolVLESS, Enable: true}
	db.Create(&ib)
	// 1 GB quota, already over
	cl := models.Client{
		InboundID: ib.ID, Email: "over@load.test", Enable: true, TotalGB: 1,
		ExpiryTime: time.Now().Add(24 * time.Hour).UnixMilli(),
		Up:         0, Down: 0, SubID: "overquota1", SubscriptionStatus: "active",
	}
	db.Create(&cl)
	stats := map[string]*xray.Traffic{
		"over@load.test": {Uplink: 900 << 20, Downlink: 200 << 20}, // > 1GiB
	}
	s := &Server{DB: db, Config: &config.Config{}}
	res := s.SyncTrafficWithStats(stats, false)
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	var got models.Client
	db.First(&got, cl.ID)
	if got.Enable {
		t.Fatalf("expected disabled over-quota client, up=%d down=%d", got.Up, got.Down)
	}
	if !res.NeedRegen {
		t.Fatal("expected NeedRegen after disable")
	}
}
