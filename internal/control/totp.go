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

// generateTOTPSecret 生成 base32 编码的 32 字节（256 位）TOTP 密钥（RFC 6238，
// 高于 RFC 要求的 128 位最低熵）。
func generateTOTPSecret() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes(32))
}

// totpCode 按 SHA-1 计算 TOTP 码。算法有意保留 SHA-1：RFC 6238 将 SHA-1 列为
// 必须支持且为默认算法，Google Authenticator 等主流认证器仅支持 SHA-1；
// HMAC-SHA1 的抗碰撞性不适用于一次性口令场景（弱点是 6 位码空间而非哈希），
// 切换为 SHA-256 反而会破坏现有用户绑定（S14 评估结论）。
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
