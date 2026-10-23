// Package accesscode管理独立于网页登录密码的只读授权码。
package accesscode

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
)

const ReadScope = "mail.read"

var ErrInvalid = errors.New("invalid or inactive access code")
var ErrParams = errors.New("invalid access code parameters")

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Create仅向调用者返回一次明文，持久化的随机码摘要不能用于认证。
func Create(userID int, name string, expiresAt int64, credentialVersion string) (models.AccessCode, string, error) {
	name = strings.TrimSpace(name)
	now := time.Now().Unix()
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || expiresAt < 0 || (expiresAt != 0 && expiresAt <= now) {
		return models.AccessCode{}, "", ErrParams
	}
	var user models.User
	ok, err := db.Instance.ID(userID).Where("disabled=0").Get(&user)
	if err != nil {
		return models.AccessCode{}, "", err
	}
	// 必须绑定发起请求时已验证的会话版本，避免改密与创建并发时旧会话签发新码。
	if !ok || subtle.ConstantTimeCompare([]byte(credentialVersion), []byte(digest(user.Password))) != 1 {
		return models.AccessCode{}, "", ErrInvalid
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return models.AccessCode{}, "", err
	}
	token := "pmr_" + base64.RawURLEncoding.EncodeToString(secret)
	code := models.AccessCode{UserID: userID, Name: name, Digest: digest(token), CredentialDigest: digest(user.Password), Scope: ReadScope, CreatedAt: now, ExpiresAt: expiresAt}
	_, err = db.Instance.Insert(&code)
	if err != nil {
		return models.AccessCode{}, "", err
	}
	return code, token, nil
}

// Authenticate每次复验用户及授权生命周期，不缓存权限，也不授予管理员身份。
func Authenticate(token string) (models.User, error) {
	if len(token) != 47 || !strings.HasPrefix(token, "pmr_") {
		return models.User{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[4:])
	if err != nil || len(raw) != 32 {
		return models.User{}, ErrInvalid
	}
	var code models.AccessCode
	ok, err := db.Instance.Where("digest=?", digest(token)).Get(&code)
	if err != nil {
		return models.User{}, err
	}
	if !ok || code.Scope != ReadScope || code.RevokedAt != 0 || (code.ExpiresAt != 0 && code.ExpiresAt <= time.Now().Unix()) {
		return models.User{}, ErrInvalid
	}
	var user models.User
	ok, err = db.Instance.ID(code.UserID).Where("disabled=0").Get(&user)
	if err != nil {
		return models.User{}, err
	}
	if !ok || subtle.ConstantTimeCompare([]byte(code.CredentialDigest), []byte(digest(user.Password))) != 1 {
		return models.User{}, ErrInvalid
	}
	return user, nil
}

type Info struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	RevokedAt int64  `json:"revoked_at"`
	Revoked   bool   `json:"revoked"`
	Expired   bool   `json:"expired"`
}

func List(userID int) ([]Info, error) {
	var user models.User
	ok, err := db.Instance.ID(userID).Get(&user)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalid
	}
	codes := []models.AccessCode{}
	if err = db.Instance.Where("user_id=?", userID).Desc("id").Find(&codes); err != nil {
		return nil, err
	}
	result := make([]Info, 0, len(codes))
	now := time.Now().Unix()
	credential := digest(user.Password)
	for _, c := range codes {
		result = append(result, Info{
			ID: c.ID, Name: c.Name, Scope: c.Scope,
			CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt, RevokedAt: c.RevokedAt,
			Revoked: c.RevokedAt != 0 || c.CredentialDigest != credential,
			Expired: c.ExpiresAt != 0 && c.ExpiresAt <= now,
		})
	}
	return result, nil
}

// Revoke必须同时按用户和授权码ID定位，管理员也不能撤销他人的码。
func Revoke(userID int, id int64) error {
	if id <= 0 {
		return ErrParams
	}
	var code models.AccessCode
	ok, err := db.Instance.ID(id).Where("user_id=?", userID).Get(&code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalid
	}
	if code.RevokedAt != 0 {
		return nil
	}
	_, err = db.Instance.ID(id).Where("user_id=?", userID).Cols("revoked_at").Update(&models.AccessCode{RevokedAt: time.Now().Unix()})
	return err
}
