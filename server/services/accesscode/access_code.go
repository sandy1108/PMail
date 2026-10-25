// Package accesscode管理独立于网页登录密码的用途授权码。
package accesscode

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
)

const ReadScope = "mail.read"
const ClientScope = "mail.client"

var ErrInvalid = errors.New("invalid or inactive access code")
var ErrParams = errors.New("invalid access code parameters")

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Create仅向调用者返回一次明文，持久化的随机码摘要不能用于认证。
func Create(userID int, name string, expiresAt int64, credentialVersion string) (models.AccessCode, string, error) {
	return CreateWithPermissions(userID, name, expiresAt, credentialVersion, []string{"http.read"})
}

// CreateWithPermissions按显式用途签发；默认兼容逻辑由调用层选择。
func CreateWithPermissions(userID int, name string, expiresAt int64, credentialVersion string, permissions []string) (models.AccessCode, string, error) {
	normalized, err := NormalizePermissions(permissions)
	if err != nil {
		return models.AccessCode{}, "", err
	}
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
	scope := ClientScope
	if len(normalized) == 1 && normalized[0] == "http.read" {
		scope = ReadScope
	}
	code := models.AccessCode{UserID: userID, Name: name, Digest: digest(token), CredentialDigest: digest(user.Password), Scope: scope, CreatedAt: now, ExpiresAt: expiresAt, Permissions: encodePermissions(normalized)}
	_, err = db.Instance.Insert(&code)
	if err != nil {
		return models.AccessCode{}, "", err
	}
	return code, token, nil
}

// Authenticate每次复验用户及授权生命周期，不缓存权限，也不授予管理员身份。
func Authenticate(token string) (models.User, error) {
	grant, err := AuthenticateFor(token, "http")
	return grant.User, err
}

// Grant不包含密钥，仅保存用于复验的记录ID和权限版本。
type Grant struct {
	User      models.User
	CodeID    int64
	Revision  int64
	HTTPWrite bool
}

func AuthenticateFor(token, protocol string) (Grant, error) {
	if len(token) != 47 || !strings.HasPrefix(token, "pmr_") {
		return Grant{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[4:])
	if err != nil || len(raw) != 32 {
		return Grant{}, ErrInvalid
	}
	var code models.AccessCode
	ok, err := db.Instance.Where("digest=?", digest(token)).Get(&code)
	if err != nil {
		return Grant{}, err
	}
	if !ok {
		return Grant{}, ErrInvalid
	}
	return validateCode(code, protocol)
}

// ValidateGrant每次复验生命周期；权限版本变化必须重新认证，不能复活旧会话。
func ValidateGrant(id, revision int64, protocol string) (Grant, error) {
	var code models.AccessCode
	ok, err := db.Instance.ID(id).Get(&code)
	if err != nil {
		return Grant{}, err
	}
	if !ok || code.Revision != revision {
		return Grant{}, ErrInvalid
	}
	return validateCode(code, protocol)
}
func validateCode(code models.AccessCode, protocol string) (Grant, error) {
	if code.RevokedAt != 0 || (code.ExpiresAt != 0 && code.ExpiresAt <= time.Now().Unix()) {
		return Grant{}, ErrInvalid
	}
	permissions, err := codePermissions(code)
	if err != nil {
		return Grant{}, ErrInvalid
	}
	allowed, write := false, false
	for _, p := range permissions {
		if protocol == "http" && (p == "http.read" || p == "http.write") {
			allowed = true
			write = p == "http.write"
		}
		if (protocol == "imap" || protocol == "pop3" || protocol == "smtp") && p == protocol {
			allowed = true
		}
	}
	if !allowed {
		return Grant{}, ErrInvalid
	}
	var user models.User
	ok, err := db.Instance.ID(code.UserID).Where("disabled=0").Get(&user)
	if err != nil {
		return Grant{}, err
	}
	if !ok || subtle.ConstantTimeCompare([]byte(code.CredentialDigest), []byte(digest(user.Password))) != 1 {
		return Grant{}, ErrInvalid
	}
	return Grant{User: user, CodeID: code.ID, Revision: code.Revision, HTTPWrite: write}, nil
}

func NormalizePermissions(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 4 {
		return nil, ErrParams
	}
	seen := map[string]bool{}
	for _, v := range values {
		if v != "http.read" && v != "http.write" && v != "imap" && v != "pop3" && v != "smtp" {
			return nil, ErrParams
		}
		if seen[v] {
			return nil, ErrParams
		}
		seen[v] = true
	}
	if seen["http.read"] && seen["http.write"] {
		return nil, ErrParams
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result, nil
}
func encodePermissions(values []string) string { raw, _ := json.Marshal(values); return string(raw) }
func codePermissions(code models.AccessCode) ([]string, error) {
	if code.Permissions == "" && code.Scope == ReadScope {
		return []string{"http.read"}, nil
	}
	var values []string
	if json.Unmarshal([]byte(code.Permissions), &values) != nil {
		return nil, ErrInvalid
	}
	return NormalizePermissions(values)
}

// UpdatePermissions不允许改变所有权或复活撤销记录；所有用途变更均轮换权限版本。
func UpdatePermissions(userID int, id int64, values []string, credentialVersion string) error {
	normalized, err := NormalizePermissions(values)
	if err != nil || id <= 0 {
		return ErrParams
	}
	var user models.User
	ok, err := db.Instance.ID(userID).Where("disabled=0").Get(&user)
	if err != nil {
		return err
	}
	if !ok || subtle.ConstantTimeCompare([]byte(credentialVersion), []byte(digest(user.Password))) != 1 {
		return ErrInvalid
	}
	n, err := db.Instance.Where("id=? AND user_id=? AND revoked_at=0 AND credential_digest=? AND (expires_at=0 OR expires_at>?)", id, userID, credentialVersion, time.Now().Unix()).Incr("revision", 1).Cols("permissions", "scope").Update(&models.AccessCode{Permissions: encodePermissions(normalized), Scope: ClientScope})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalid
	}
	return nil
}

type Info struct {
	Permissions []string `json:"permissions"`
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Scope       string   `json:"scope"`
	CreatedAt   int64    `json:"created_at"`
	ExpiresAt   int64    `json:"expires_at"`
	RevokedAt   int64    `json:"revoked_at"`
	Revoked     bool     `json:"revoked"`
	Expired     bool     `json:"expired"`
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
		permissions, _ := codePermissions(c)
		result = append(result, Info{
			ID: c.ID, Name: c.Name, Scope: c.Scope, Permissions: permissions,
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
