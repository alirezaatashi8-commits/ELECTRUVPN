package telegram

import "sync"

var (
	pendingLoyaltyMu    sync.Mutex
	pendingLoyalty      = map[int64]bool{}
	pendingGiftRedeemMu sync.Mutex
	pendingGiftRedeem   = map[int64]bool{}
)

func SetPendingLoyalty(tgID int64, on bool) {
	pendingLoyaltyMu.Lock()
	defer pendingLoyaltyMu.Unlock()
	if on {
		pendingLoyalty[tgID] = true
	} else {
		delete(pendingLoyalty, tgID)
	}
}

func GetPendingLoyalty(tgID int64) bool {
	pendingLoyaltyMu.Lock()
	defer pendingLoyaltyMu.Unlock()
	return pendingLoyalty[tgID]
}

func SetPendingGiftRedeem(tgID int64, on bool) {
	pendingGiftRedeemMu.Lock()
	defer pendingGiftRedeemMu.Unlock()
	if on {
		pendingGiftRedeem[tgID] = true
	} else {
		delete(pendingGiftRedeem, tgID)
	}
}

func IsPendingGiftRedeem(tgID int64) bool {
	pendingGiftRedeemMu.Lock()
	defer pendingGiftRedeemMu.Unlock()
	return pendingGiftRedeem[tgID]
}
