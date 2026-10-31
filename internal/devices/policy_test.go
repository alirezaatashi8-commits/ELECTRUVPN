package devices

import (
	"fmt"
	"testing"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func tdb(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ClientDevice{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLimitBlocksNewDevice(t *testing.T) {
	db := tdb(t)
	cl := models.Client{ID: 1, MaxActiveSessions: 1}
	now := time.Now()
	db.Create(&models.ClientDevice{ClientID: 1, DeviceID: "a", Active: true, LastSeenAt: now})
	if err := AllowAccess(db, cl, "b"); err != ErrLimitReached {
		t.Fatalf("want limit, got %v", err)
	}
	if err := AllowAccess(db, cl, "a"); err != nil {
		t.Fatalf("known device: %v", err)
	}
}

func TestRevokedBlocked(t *testing.T) {
	db := tdb(t)
	cl := models.Client{ID: 2, MaxActiveSessions: 5}
	now := time.Now()
	db.Create(&models.ClientDevice{ClientID: 2, DeviceID: "x", Active: false, RevokedAt: &now, LastSeenAt: now})
	if err := AllowAccess(db, cl, "x"); err != ErrRevoked {
		t.Fatalf("want revoked, got %v", err)
	}
}

func TestRegisterRespectsLimit(t *testing.T) {
	db := tdb(t)
	cl := models.Client{ID: 3, MaxActiveSessions: 1}
	d1 := &models.ClientDevice{DeviceID: "d1", Name: "phone"}
	if err := RegisterOrTouch(db, cl, d1); err != nil {
		t.Fatal(err)
	}
	d2 := &models.ClientDevice{DeviceID: "d2", Name: "pc"}
	if err := RegisterOrTouch(db, cl, d2); err != ErrLimitReached {
		t.Fatalf("want limit, got %v", err)
	}
}

func TestRevokeFreesSlot(t *testing.T) {
	db := tdb(t)
	cl := models.Client{ID: 4, MaxActiveSessions: 1}
	d1 := &models.ClientDevice{DeviceID: "d1"}
	_ = RegisterOrTouch(db, cl, d1)
	ok, err := Revoke(db, 4, d1.ID)
	if err != nil || !ok {
		t.Fatalf("revoke %v %v", ok, err)
	}
	d2 := &models.ClientDevice{DeviceID: "d2"}
	if err := RegisterOrTouch(db, cl, d2); err != nil {
		t.Fatal(err)
	}
}
