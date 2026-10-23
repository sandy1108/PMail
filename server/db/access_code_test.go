package db

import (
	"path/filepath"
	"testing"

	"github.com/Jinnrry/pmail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestAccessCodeSchemaUpgradePreservesData(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := Instance
	Instance = engine
	t.Cleanup(func() { engine.Close(); Instance = old })
	if err = engine.Sync2(&models.User{}, &models.Email{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "existing", Name: "Existing", Password: "synthetic hash"}
	email := models.Email{Subject: "preserve me"}
	engine.Insert(&user)
	engine.Insert(&email)
	syncTables()
	syncTables()
	ok, err := engine.IsTableExist(&models.AccessCode{})
	if err != nil || !ok {
		t.Fatal("access code table not created", err)
	}
	var saved models.Email
	ok, err = engine.ID(email.Id).Get(&saved)
	if err != nil || !ok || saved.Subject != email.Subject {
		t.Fatal("existing mail changed", err)
	}
	var savedUser models.User
	ok, err = engine.ID(user.ID).Get(&savedUser)
	if err != nil || !ok || savedUser.Password != user.Password {
		t.Fatal("existing account changed", err)
	}
}
