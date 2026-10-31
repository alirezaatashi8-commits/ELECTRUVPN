package telegram

import "sync"

var (
	customGBMu      sync.Mutex
	pendingCustomGB = map[int64]bool{} // user is typing GB amount
)

func SetPendingCustomGB(tgID int64, on bool) {
	customGBMu.Lock()
	if on {
		pendingCustomGB[tgID] = true
	} else {
		delete(pendingCustomGB, tgID)
	}
	customGBMu.Unlock()
}

func IsPendingCustomGB(tgID int64) bool {
	customGBMu.Lock()
	defer customGBMu.Unlock()
	return pendingCustomGB[tgID]
}
