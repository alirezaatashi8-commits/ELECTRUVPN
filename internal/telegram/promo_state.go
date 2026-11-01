package telegram

import "sync"

var (
	pendingPromoMu sync.Mutex
	pendingPromo   = map[int64]string{} // tgUserID → code
)

func SetPendingPromo(tgID int64, code string) {
	pendingPromoMu.Lock()
	defer pendingPromoMu.Unlock()
	if code == "" {
		delete(pendingPromo, tgID)
		return
	}
	pendingPromo[tgID] = code
}

func GetPendingPromo(tgID int64) string {
	pendingPromoMu.Lock()
	defer pendingPromoMu.Unlock()
	return pendingPromo[tgID]
}
