package models

import "time"

// EmailAlias只描述收件地址归属，不参与任何登录认证。
type EmailAlias struct {
	ID        int64     `xorm:"id pk autoincr" json:"id"`
	UserID    int       `xorm:"user_id notnull index" json:"-"`
	LocalPart string    `xorm:"local_part varchar(64) notnull index" json:"local_part"`
	Domain    string    `xorm:"domain varchar(253) notnull" json:"domain"`
	Address   string    `xorm:"address varchar(254) notnull unique" json:"address"`
	Remark    string    `xorm:"remark varchar(200) notnull default('')" json:"remark"`
	Enabled   bool      `xorm:"enabled notnull" json:"enabled"`
	CreatedAt time.Time `xorm:"created_at created" json:"created_at"`
	UpdatedAt time.Time `xorm:"updated_at updated" json:"updated_at"`
}

func (EmailAlias) TableName() string { return "email_alias" }
