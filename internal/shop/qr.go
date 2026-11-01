package shop

import (
	qrcode "github.com/skip2/go-qrcode"
)

// QRCodePNG encodes content as a PNG QR image.
func QRCodePNG(content string, size int) ([]byte, error) {
	if size < 128 {
		size = 256
	}
	return qrcode.Encode(content, qrcode.Medium, size)
}
