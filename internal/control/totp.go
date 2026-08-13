package control

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// generateTOTPSecret 生成 base32 编码的 20 字节 TOTP 密钥（RFC 6238）。
func generateTOTPSecret() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes(20))
}

func totpCode(secret string, at time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	counter := uint64(at.Unix() / 30)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0xf
	code := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000), nil
}

// verifyTOTP 校验一次性密码，允许 ±1 个时间窗口的时钟漂移。
func verifyTOTP(secret, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for _, drift := range []int64{-1, 0, 1} {
		expected, err := totpCode(secret, at.Add(time.Duration(drift)*30*time.Second))
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// otpauthURI 生成扫码用的 otpauth:// 地址。
func otpauthURI(secret, email string) string {
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", "WireMesh")
	return "otpauth://totp/" + url.PathEscape("WireMesh:"+email) + "?" + params.Encode()
}
