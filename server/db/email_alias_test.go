package db

import (
	"github.com/Jinnrry/pmail/models"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"xorm.io/xorm"
)

func TestAliasSchemaUpgradePreservesExistingMail(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := Instance
	Instance = engine
	t.Cleanup(func() { engine.Close(); Instance = old })
	if err = engine.Sync2(&models.User{}, &models.Email{}, &models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Exec(`CREATE TABLE user_email (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, email_id INTEGER NOT NULL, is_read INTEGER DEFAULT 0, group_id INTEGER DEFAULT 0, status INTEGER DEFAULT 0, "create" DATETIME)`); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "existing", Password: "synthetic-hash"}
	email := models.Email{Subject: "retained"}
	if _, err = engine.Insert(&user, &email); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Exec(`INSERT INTO user_email(user_id,email_id) VALUES(?,?)`, user.ID, email.Id); err != nil {
		t.Fatal(err)
	}
	syncTables()
	syncTables()
	var relation models.UserEmail
	if found, err := engine.Where("user_id=? AND email_id=?", user.ID, email.Id).Get(&relation); err != nil || !found || relation.DeliveryAddresses != "" {
		t.Fatal("old association changed", relation, err)
	}
	var saved models.User
	engine.ID(user.ID).Get(&saved)
	if saved.Password != "synthetic-hash" {
		t.Fatal("credential changed")
	}
	var savedEmail models.Email
	engine.ID(email.Id).Get(&savedEmail)
	if savedEmail.Subject != "retained" {
		t.Fatal("mail changed")
	}
	if exists, err := engine.IsTableExist(&models.EmailAlias{}); err != nil || !exists {
		t.Fatal("alias table missing", err)
	}
}
