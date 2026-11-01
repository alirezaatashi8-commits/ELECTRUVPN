package routing

import (
	"testing"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSelectNodes_SkipsUnhealthyAgent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:fo?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&models.ServerNode{}, &models.AgentNode{}, &models.NodeGroupMember{}, &models.NodePerformanceSnapshot{})
	good := models.ServerNode{Name: "good", Address: "1.1.1.1", Port: 443, Enabled: true, DrainState: "active"}
	bad := models.ServerNode{Name: "bad", Address: "2.2.2.2", Port: 443, Enabled: true, DrainState: "active"}
	db.Create(&good)
	db.Create(&bad)
	now := time.Now()
	old := now.Add(-10 * time.Minute)
	db.Create(&models.AgentNode{NodeID: good.ID, LastSeenAt: &now})
	db.Create(&models.AgentNode{NodeID: bad.ID, LastSeenAt: &old, LastError: "timeout"})
	cands, err := SelectNodes(db, SelectOptions{Failover: true, Strategy: "least_load", MaxNodes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) == 0 {
		t.Fatal("expected at least one candidate")
	}
	if cands[0].NodeID != good.ID {
		t.Fatalf("expected healthy node first, got %d (%s)", cands[0].NodeID, cands[0].Name)
	}
	for _, c := range cands {
		if c.NodeID == bad.ID && c.Healthy {
			t.Fatal("bad node must not be healthy")
		}
	}
}
