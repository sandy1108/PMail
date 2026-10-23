package password

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"golang.org/x/crypto/argon2"
	"strings"
)

// 固定并校验参数，避免数据库中的异常哈希触发任意内存分配。仅适用于新初始化账号。
const prefix = "$argon2id$v=19$m=32768,t=3,p=1$"

var workers = make(chan struct{}, 2)

func Encode(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	workers <- struct{}{}
	defer func() { <-workers }()
	key := argon2.IDKey([]byte(password), salt, 3, 32768, 1, 32)
	return prefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
}

func Verify(encoded, password string) bool {
	if !strings.HasPrefix(encoded, prefix) || len(password) > 4096 {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, prefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[0])
	want, e2 := base64.RawStdEncoding.DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	workers <- struct{}{}
	defer func() { <-workers }()
	got := argon2.IDKey([]byte(password), salt, 3, 32768, 1, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// 非口令用途的历史摘要工具；认证入口不得使用此函数。
func Md5Encode(str string) string {
	h := md5.Sum([]byte(str))
	return hex.EncodeToString(h[:])
}
