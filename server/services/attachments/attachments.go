package attachments

import (
	"encoding/json"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/auth"
	"github.com/Jinnrry/pmail/utils/context"
	log "github.com/sirupsen/logrus"
)

func GetAttachments(ctx *context.Context, emailId int, cid string) (string, []byte) {

	// 获取邮件内容
	var email models.Email
	_, err := db.Instance.ID(emailId).Get(&email)
	if err != nil {
		log.WithContext(ctx).Errorf("SQL error:%+v", err)
		return "", nil
	}

	// 检查权限
	if !auth.HasAuth(ctx, &email) {
		return "", nil
	}

	var atts []parsemail.Attachment
	_ = json.Unmarshal([]byte(email.Attachments), &atts)
	for _, att := range atts {
		if att.ContentID == cid {
			return att.ContentType, att.Content
		}
	}
	return "", nil
}

func GetAttachmentsByIndex(ctx *context.Context, emailId int, index int) (string, []byte) {

	// 获取邮件内容
	var email models.Email
	_, err := db.Instance.ID(emailId).Get(&email)
	if err != nil {
		log.WithContext(ctx).Errorf("SQL error:%+v", err)
		return "", nil
	}

	// 检查权限
	if !auth.HasAuth(ctx, &email) {
		return "", nil
	}

	var atts []parsemail.Attachment
	_ = json.Unmarshal([]byte(email.Attachments), &atts)

	if index >= 0 && len(atts) > index {
		// 非nil空切片表示找到了零字节附件，nil仍表示不存在或无权限。
		return atts[index].Filename, append([]byte{}, atts[index].Content...)
	}
	return "", nil
}
