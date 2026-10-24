package smtp_server

import (
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/Jinnrry/pmail/consts"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/hooks"
	"github.com/Jinnrry/pmail/hooks/framework"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/mailaddress"
	"github.com/Jinnrry/pmail/utils/async"
	"github.com/Jinnrry/pmail/utils/context"
	smtp "github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

const deliveryPendingError = "delivery result pending; outcome unknown after interruption"

type outgoingSender func(*context.Context, *parsemail.Email) (error, map[string]error)

func (s *Session) deliverOutgoingEmail(email *parsemail.Email, sender outgoingSender) error {
	ctx := s.Ctx
	_, saved, err := saveEmail(ctx, email.Size, email, ctx.UserID, int(consts.EmailTypeSend), nil, true, true)
	if err != nil {
		log.WithContext(ctx).Errorf("CRITICAL audit_event=initial_persist_failed direction=outbound msg_id=%q error=%v", email.MsgID, err)
		return localPersistenceSMTPError()
	}

	deliveryErr, domainErrors := sender(ctx, email)
	runSendAfterHooks(ctx, email, domainErrors)

	targetStatus := consts.EmailStatusSent
	var errorText any
	if deliveryErr != nil {
		targetStatus = consts.EmailStatusFail
		errorText = deliveryErr.Error()
	}

	if err = persistDeliveryResult(ctx, saved.Id, targetStatus, errorText); err != nil {
		log.WithContext(ctx).Errorf(
			"CRITICAL audit_event=delivery_result_persist_failed email_id=%d msg_id=%q target_status=%d downstream_accepted=%t error=%v",
			saved.Id, saved.MsgID, targetStatus, deliveryErr == nil, err,
		)
	}

	// If the remote MX accepted DATA, returning a local 451 here would cause a
	// duplicate delivery. The persistence failure is retained as pending state
	// and a CRITICAL log; the SMTP result must still reflect the remote result.
	return downstreamDeliverySMTPError(deliveryErr, domainErrors)
}

func runSendAfterHooks(ctx *context.Context, email *parsemail.Email, domainErrors map[string]error) {
	process := async.New(ctx)
	for _, hook := range hooks.HookList {
		if hook == nil {
			continue
		}
		process.WaitProcess(func(value any) {
			value.(framework.EmailHook).SendAfter(ctx, email, domainErrors)
		}, hook)
	}
	process.Wait()
}

func persistDeliveryResult(ctx *context.Context, emailID int, status int8, errorText any) error {
	return db.Transaction(db.Instance, func(session *xorm.Session) error {
		result, err := session.Exec(db.WithContext(ctx, `update email
			set status=?, error=?, update_time=CURRENT_TIMESTAMP
			where id=?`), status, errorText, emailID)
		if err != nil {
			return err
		}
		if err = requireOneAffected(result, "update email delivery result"); err != nil {
			return err
		}

		result, err = session.Exec(db.WithContext(ctx, `update user_email
			set status=? where email_id=?`), status, emailID)
		if err != nil {
			return err
		}
		return requireOneAffected(result, "update user_email delivery result")
	})
}

func requireOneAffected(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%s: affected %d rows, want 1", operation, affected)
	}
	return nil
}

func localPersistenceSMTPError() error {
	return &smtp.SMTPError{
		Code:         451,
		EnhancedCode: smtp.EnhancedCode{4, 3, 0},
		Message:      "Temporary local persistence failure",
	}
}

func persistInitialAudit(ctx *context.Context, modelEmail models.Email, userIDs []int) (*models.Email, error) {
	var saved models.Email
	err := db.Transaction(db.Instance, func(session *xorm.Session) error {
		candidate := modelEmail
		candidate.Id = 0
		if _, err := session.Insert(&candidate); err != nil {
			return err
		}
		if candidate.Id <= 0 {
			return stderrors.New("email insert returned no id")
		}
		for _, userID := range userIDs {
			relation := models.UserEmail{EmailID: candidate.Id, UserID: userID, Status: candidate.Status}
			if _, err := session.Insert(&relation); err != nil {
				return err
			}
		}
		saved = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// persistIncomingAudit在同一事务中复验RCPT归属、保存正文和用户关联。
// 地址变更与本过程共用写锁，防止删除重建后错投给其他用户。
func persistIncomingAudit(ctx *context.Context, modelEmail models.Email, addresses []string, accepted []mailaddress.Recipient) ([]*models.User, *models.Email, error) {
	var saved models.Email
	var users []*models.User
	err := mailaddress.WithWrite(func() error {
		return db.Transaction(db.Instance, func(session *xorm.Session) error {
			users = nil
			if len(addresses) == 0 || (accepted != nil && len(accepted) != len(addresses)) {
				return mailaddress.ErrRecipient
			}
			destinations := map[int][]string{}
			for i, address := range addresses {
				recipient, err := mailaddress.ResolveIn(session, address)
				if err != nil {
					return err
				}
				if accepted != nil && (accepted[i].Address != recipient.Address || accepted[i].User.ID != recipient.User.ID || accepted[i].AliasID != recipient.AliasID) {
					return mailaddress.ErrRecipient
				}
				if _, exists := destinations[recipient.User.ID]; !exists {
					user := recipient.User
					users = append(users, &user)
				}
				duplicate := false
				for _, previous := range destinations[recipient.User.ID] {
					if previous == recipient.Address {
						duplicate = true
						break
					}
				}
				if !duplicate {
					destinations[recipient.User.ID] = append(destinations[recipient.User.ID], recipient.Address)
				}
			}
			candidate := modelEmail
			candidate.Id = 0
			if _, err := session.Insert(&candidate); err != nil {
				return err
			}
			if candidate.Id <= 0 {
				return stderrors.New("email insert returned no id")
			}
			for _, user := range users {
				relation := models.UserEmail{UserID: user.ID, EmailID: candidate.Id, Status: candidate.Status, DeliveryAddresses: json2string(destinations[user.ID])}
				if _, err := session.Insert(&relation); err != nil {
					return err
				}
			}
			saved = candidate
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return users, &saved, nil
}

func boolInt8(value bool) int8 {
	if value {
		return 1
	}
	return 0
}

func json2string(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
