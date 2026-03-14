package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ComputeHTTPSignature 计算 HTTP 请求签名
//
// 签名算法与 JS runtime/src/auth/signer.js 保持一致：
//
//	payload = "{METHOD}&{path}&{agentId}&{timestampSec}"
//	signature = HMAC-SHA256(agentKey, payload).hex()
func ComputeHTTPSignature(agentKey, method, path, agentID, timestampSec string) string {
	payload := fmt.Sprintf("%s&%s&%s&%s",
		strings.ToUpper(method),
		path,
		agentID,
		timestampSec,
	)
	mac := hmac.New(sha256.New, []byte(agentKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
