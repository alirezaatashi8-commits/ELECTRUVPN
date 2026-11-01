package reseller

import (
	"testing"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.ResellerLedger{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateAndScope(t *testing.T) {
	db := testDB(t)
	owner := models.User{Username: "owner", Role: "owner", Enabled: true}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	hash := "x"
	r1, err := CreateReseller(db, &owner, "r1", hash, CreateOpts{
		MaxTotalGB: 100, MaxClients: 10, InitialCredit: 5000, AllowSubResellers: true, MaxSubResellers: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.ResellerLevel != 1 || r1.CreditBalance != 5000 {
		t.Fatalf("unexpected r1: %+v", r1)
	}
	// refresh r1 credit after topup
	_ = db.First(r1, r1.ID)
	sub, err := CreateReseller(db, r1, "sub1", hash, CreateOpts{MaxTotalGB: 20, MaxClients: 5, InitialCredit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ParentUserID != r1.ID || sub.ResellerLevel != 2 {
		t.Fatalf("sub hierarchy: %+v", sub)
	}
	_ = db.First(r1, r1.ID)
	if r1.CreditBalance != 4000 {
		t.Fatalf("parent credit after transfer want 4000 got %d", r1.CreditBalance)
	}
	ids, filter, err := ScopeUserIDs(db, r1)
	if err != nil || !filter {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("scope ids %v", ids)
	}
}

func TestInsufficientCredit(t *testing.T) {
	db := testDB(t)
	owner := models.User{Username: "o", Role: "owner", Enabled: true}
	_ = db.Create(&owner)
	r, err := CreateReseller(db, &owner, "poor", "h", CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := DebitForClient(db, r, 10, "c1"); err != ErrInsufficient {
		t.Fatalf("want insufficient got %v", err)
	}
}

func TestRefundClientGB(t *testing.T) {
	db := testDB(t)
	owner := models.User{Username: "own2", Role: "owner", Enabled: true}
	_ = db.Create(&owner)
	// set global wholesale via panel_settings table
	_ = db.Exec("CREATE TABLE IF NOT EXISTS panel_settings (id integer primary key, key text, value text, updated_at datetime)")
	_ = db.Exec("INSERT INTO panel_settings (key, value) VALUES (?, ?)", models.ResellerWholesalePricePerGBKey, "1000")
	r, err := CreateReseller(db, &owner, "rr", "h", CreateOpts{InitialCredit: 50000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ChargeForClientGB(db, r, 5, "c1"); err != nil {
		t.Fatal(err)
	}
	_ = db.First(r, r.ID)
	if r.CreditBalance != 45000 {
		t.Fatalf("after charge want 45000 got %d", r.CreditBalance)
	}
	if _, err := RefundClientGB(db, r.ID, 5, "c1del"); err != nil {
		t.Fatal(err)
	}
	_ = db.First(r, r.ID)
	if r.CreditBalance != 50000 {
		t.Fatalf("after refund want 50000 got %d", r.CreditBalance)
	}
}
