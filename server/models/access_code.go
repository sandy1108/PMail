package models

// AccessCode仅存高熵密钥的摘要；口令版本绑定保证改密后旧码立即失效。
type AccessCode struct {
	ID               int64  `xorm:"id pk autoincr" json:"id"`
	UserID           int    `xorm:"user_id notnull index" json:"-"`
	Permissions      string `xorm:"TEXT" json:"-"`               // 空值仅用于兼容旧HTTP只读码
	Revision         int64  `xorm:"notnull default(0)" json:"-"` // 用途修改后使旧协议会话失效
	Name             string `xorm:"varchar(80) notnull" json:"name"`
	Digest           string `xorm:"char(64) notnull unique" json:"-"`
	CredentialDigest string `xorm:"char(64) notnull" json:"-"`
	Scope            string `xorm:"varchar(32) notnull" json:"scope"`
	CreatedAt        int64  `xorm:"notnull" json:"created_at"`
	ExpiresAt        int64  `xorm:"notnull" json:"expires_at"`
	RevokedAt        int64  `xorm:"notnull" json:"revoked_at"`
}

func (AccessCode) TableName() string { return "access_code" }
