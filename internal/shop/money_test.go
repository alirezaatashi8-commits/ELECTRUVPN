package shop

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Wallet{}, &models.WalletLedger{}, &models.ShopOrder{},
		&models.ShopPlan{}, &models.Client{}, &models.Inbound{},
		&models.TelegramCustomer{}, &models.PanelSetting{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedInbound(t *testing.T, db *gorm.DB) models.Inbound {
	t.Helper()
	ib := models.Inbound{Remark: "t", Port: 443, Protocol: models.ProtocolVLESS, Enable: true}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	return ib
}

func TestDebitAndFulfill_InsufficientBalance(t *testing.T) {
	db := testDB(t)
	ib := seedInbound(t, db)
	_, _ = GetOrCreateWallet(db, 42)
	order := &models.ShopOrder{
		TelegramUserID: 42, TelegramChatID: 42,
		PlanName: "p", TotalGB: 10, DurationDays: 30, PriceToman: 50000,
		InboundID: ib.ID, Status: "pending_review", Kind: "plan",
	}
	_, err := DebitAndFulfill(db, order, "buy", "t", "t")
	if err == nil {
		t.Fatal("expected insufficient funds")
	}
	var n int64
	db.Model(&models.ShopOrder{}).Count(&n)
	if n != 0 {
		t.Fatalf("orphan order created: %d", n)
	}
}

func TestDebitAndFulfill_Success(t *testing.T) {
	db := testDB(t)
	ib := seedInbound(t, db)
	_, err := CreditWallet(db, 7, 100000, "topup", "t", "t")
	if err != nil {
		t.Fatal(err)
	}
	order := &models.ShopOrder{
		TelegramUserID: 7, TelegramChatID: 7,
		PlanName: "p", TotalGB: 5, DurationDays: 30, PriceToman: 40000,
		InboundID: ib.ID, Status: "pending_review", Kind: "plan",
	}
	cl, err := DebitAndFulfill(db, order, "buy", "t", "t")
	if err != nil {
		t.Fatal(err)
	}
	if cl.ID == 0 || order.Status != "approved" {
		t.Fatalf("client=%d status=%s", cl.ID, order.Status)
	}
	w, _ := GetOrCreateWallet(db, 7)
	if w.BalanceToman != 60000 {
		t.Fatalf("balance %d", w.BalanceToman)
	}
}

func TestTryAutoRenew_NotDue(t *testing.T) {
	db := testDB(t)
	ib := seedInbound(t, db)
	cl := models.Client{
		InboundID: ib.ID, Email: "u@t", Enable: true, TotalGB: 10,
		ExpiryTime: time.Now().Add(48 * time.Hour).UnixMilli(),
		SubID:      "abc", SubscriptionStatus: "active",
	}
	db.Create(&cl)
	plan := models.ShopPlan{Name: "p", TotalGB: 10, DurationDays: 30, PriceToman: 1000, InboundID: ib.ID, Enabled: true}
	db.Create(&plan)
	link := models.TelegramCustomer{
		TelegramUserID: 9, TelegramChatID: 9, ClientID: cl.ID,
		AutoRenewEnabled: true, AutoRenewPlanID: plan.ID,
	}
	db.Create(&link)
	CreditWallet(db, 9, 100000, "topup", "t", "t")
	ok, msg := TryAutoRenew(db, link)
	if ok || msg != "not due" {
		t.Fatalf("ok=%v msg=%s", ok, msg)
	}
}

func TestTryAutoRenew_RevokedSkipped(t *testing.T) {
	db := testDB(t)
	ib := seedInbound(t, db)
	cl := models.Client{
		InboundID: ib.ID, Email: "r@t", Enable: false, TotalGB: 1,
		ExpiryTime: time.Now().Add(-time.Hour).UnixMilli(),
		SubID:      "rev", SubscriptionStatus: "revoked",
	}
	db.Create(&cl)
	plan := models.ShopPlan{Name: "p", TotalGB: 1, DurationDays: 1, PriceToman: 1000, InboundID: ib.ID, Enabled: true}
	db.Create(&plan)
	link := models.TelegramCustomer{
		TelegramUserID: 11, ClientID: cl.ID, AutoRenewEnabled: true, AutoRenewPlanID: plan.ID,
	}
	db.Create(&link)
	CreditWallet(db, 11, 100000, "topup", "t", "t")
	ok, msg := TryAutoRenew(db, link)
	if ok || msg != "manually disabled" {
		t.Fatalf("ok=%v msg=%s", ok, msg)
	}
}

func TestFulfillOrder_IdempotentConcurrent(t *testing.T) {
	db := testDB(t)
	ib := seedInbound(t, db)
	order := models.ShopOrder{
		TelegramUserID: 3, TelegramChatID: 3,
		PlanName: "p", TotalGB: 2, DurationDays: 7, PriceToman: 0,
		InboundID: ib.ID, Status: "pending_review", Kind: "plan",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o := order
			_, _, err := FulfillOrder(db, &o)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var clients int64
	db.Model(&models.Client{}).Count(&clients)
	if clients != 1 {
		t.Fatalf("want 1 client got %d", clients)
	}
	for err := range errs {
		if err != nil {
			// concurrent may hit status race — only approved path should win
			t.Log("err", err)
		}
	}
	var o models.ShopOrder
	db.First(&o, order.ID)
	if o.Status != "approved" {
		t.Fatalf("status %s", o.Status)
	}
}
