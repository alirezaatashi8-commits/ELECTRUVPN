package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ValidateWebAppInitData verifies Telegram Mini App initData (HMAC-SHA256).
// Returns telegram user id and username on success.
func ValidateWebAppInitData(initData, botToken string, maxAge time.Duration) (userID int64, username string, err error) {
	if strings.TrimSpace(initData) == "" || strings.TrimSpace(botToken) == "" {
		return 0, "", fmt.Errorf("missing initData or token")
	}
	values, err := url.ParseQuery(initData)
	if err != nil {
		return 0, "", err
	}
	hash := values.Get("hash")
	if hash == "" {
		return 0, "", fmt.Errorf("missing hash")
	}
	// data-check-string
	var pairs []string
	for k, vs := range values {
		if k == "hash" {
			continue
		}
		if len(vs) > 0 {
			pairs = append(pairs, k+"="+vs[0])
		}
	}
	sort.Strings(pairs)
	check := strings.Join(pairs, "\n")
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	secretKey := secret.Sum(nil)
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(check))
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(hash)) != 1 {
		return 0, "", fmt.Errorf("invalid signature")
	}
	authDate := values.Get("auth_date")
	if authDate == "" {
		return 0, "", fmt.Errorf("missing auth_date")
	}
	if maxAge > 0 {
		sec, _ := strconv.ParseInt(authDate, 10, 64)
		if sec <= 0 {
			return 0, "", fmt.Errorf("invalid auth_date")
		}
		if time.Since(time.Unix(sec, 0)) > maxAge {
			return 0, "", fmt.Errorf("initData expired")
		}
	}
	userJSON := values.Get("user")
	if userJSON == "" {
		return 0, "", fmt.Errorf("missing user")
	}
	var u struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}
	if err := json.Unmarshal([]byte(userJSON), &u); err != nil {
		return 0, "", err
	}
	return u.ID, u.Username, nil
}
