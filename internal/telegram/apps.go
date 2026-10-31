package telegram

// Store / deep-link URLs for NapsternetV (open store app for install).
const (
	// HTTPS — works everywhere (browser, Telegram, desktop).
	AppNapsternetAndroidURL = "https://play.google.com/store/apps/details?id=com.napsternetlabs.napsternetv"
	AppNapsternetIOSURL     = "https://apps.apple.com/us/app/npv-tunnel/id1629465476"
	// Deep links — open Play Store / App Store app directly when possible.
	AppNapsternetAndroidMarket = "market://details?id=com.napsternetlabs.napsternetv"
	AppNapsternetIOSITMS       = "itms-apps://apps.apple.com/app/id1629465476"
)

func recommendedAppsMessage() string {
	return `📲 <b>دانلود مستقیم اپ پیشنهادی</b>

اپ <b>NapsternetV</b> برای اتصال به کانفیگ‌ها پیشنهاد می‌شود.

روی دکمه سیستم‌عامل خود بزنید تا <b>مستقیم</b> به صفحه دانلود / نصب بروید:

🤖 اندروید → Google Play
🍎 آیفون → App Store`
}

func recommendedAppsKeyboard() *InlineKeyboardMarkup {
	// Telegram URL buttons open the link immediately on tap (no extra step).
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "⬇️ دانلود مستقیم اندروید", URL: AppNapsternetAndroidURL}},
		{{Text: "⬇️ دانلود مستقیم iOS", URL: AppNapsternetIOSURL}},
		{{Text: "◀️ منو", CallbackData: "menu"}},
	}}
}
