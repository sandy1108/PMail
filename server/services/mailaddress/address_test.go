package mailaddress

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func aliasDB(t *testing.T) (*xorm.Engine, models.User, models.User, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aliases.db")
	engine, err := xorm.NewEngine("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance = engine
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com", "example.net"}}
	t.Cleanup(func() { db.Instance.Close(); db.Instance = oldDB; config.Instance = oldConfig })
	if err = engine.Sync2(&models.User{}, &models.EmailAlias{}); err != nil {
		t.Fatal(err)
	}
	one, two := models.User{Account: "owner", Name: "Owner"}, models.User{Account: "other", Name: "Other"}
	if _, err = engine.Insert(&one, &two); err != nil {
		t.Fatal(err)
	}
	return engine, one, two, path
}
func TestAliasOwnershipLifecycleAndPersistence(t *testing.T) {
	engine, one, two, path := aliasDB(t)
	a, err := Create(one.ID, " Shop.Ping ", "EXAMPLE.COM", "购物")
	if err != nil {
		t.Fatal(err)
	}
	if a.Address != "shop.ping@example.com" {
		t.Fatal(a.Address)
	}
	r, err := Resolve("SHOP.PING@example.com")
	if err != nil || r.User.ID != one.ID {
		t.Fatal(r, err)
	}
	page, err := List(two.ID, 1, 20, "")
	if err != nil || len(page.List) != 0 {
		t.Fatal("cross-user list", page, err)
	}
	if err = Update(two.ID, a.ID, "stolen", true); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user edit", err)
	}
	if err = Delete(two.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user delete", err)
	}
	if err = Update(one.ID, a.ID, "暂停", false); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(a.Address); !errors.Is(err, ErrRecipient) {
		t.Fatal("disabled alias accepted", err)
	}
	if _, err = Create(two.ID, "shop.ping", "example.com", ""); !errors.Is(err, ErrConflict) {
		t.Fatal("disabled alias lost reservation", err)
	}
	if err = Update(one.ID, a.ID, "恢复", true); err != nil {
		t.Fatal(err)
	}
	if err = Update(one.ID, a.ID, "恢复", true); err != nil {
		t.Fatal("repeated update must succeed", err)
	}
	if _, err = engine.ID(one.ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(a.Address); !errors.Is(err, ErrRecipient) {
		t.Fatal("disabled owner accepted", err)
	}
	if _, err = Create(one.ID, "disabled-owner", "example.com", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	engine.ID(one.ID).Cols("disabled").Update(&models.User{Disabled: 0})
	if err = engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := xorm.NewEngine("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.Instance = reopened
	if r, err = Resolve(a.Address); err != nil || r.User.ID != one.ID {
		t.Fatal("not durable", err)
	}
	if err = Delete(one.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(a.Address); !errors.Is(err, ErrRecipient) {
		t.Fatal("deleted alias accepted", err)
	}
	if _, err = Create(two.ID, "shop.ping", "example.com", ""); err != nil {
		t.Fatal("deleted address not reusable", err)
	}
}
func TestAliasNamespaceValidationAndPagination(t *testing.T) {
	_, one, two, _ := aliasDB(t)
	for _, local := range []string{"owner", "OwNeR", "other"} {
		if _, err := Create(one.ID, local, "example.net", ""); !errors.Is(err, ErrConflict) {
			t.Fatal(local, err)
		}
	}
	for _, v := range []struct{ local, domain, remark string }{
		{"", "example.com", ""}, {"bad@name", "example.com", ""}, {".start", "example.com", ""}, {"a..b", "example.com", ""}, {"end.", "example.com", ""}, {"中文", "example.com", ""}, {strings.Repeat("a", 65), "example.com", ""}, {"valid", "external.test", ""}, {"valid", "example.com", strings.Repeat("字", 201)},
	} {
		if _, err := Create(one.ID, v.local, v.domain, v.remark); !errors.Is(err, ErrParams) {
			t.Fatal(v, err)
		}
	}
	if _, err := Create(one.ID, "reserved", "example.com", ""); err != nil {
		t.Fatal(err)
	}
	if err := InsertUser(&models.User{Account: "RESERVED"}); !errors.Is(err, ErrConflict) {
		t.Fatal("user stole alias", err)
	}
	if _, err := Create(two.ID, "reserved", "example.net", ""); err != nil {
		t.Fatal("different domain full address should be distinct", err)
	}
	r, err := Resolve("reserved@example.net")
	if err != nil || r.User.ID != two.ID {
		t.Fatal("domain ignored", r, err)
	}
	for i := 0; i < 125; i++ {
		if _, err := Create(one.ID, fmt.Sprintf("address%d", i), "example.com", "batch"); err != nil {
			t.Fatal(err)
		}
	}
	page, err := List(one.ID, 2, 100, "")
	if err != nil || page.Total != 126 || len(page.List) != 26 {
		t.Fatal(page, err)
	}
	page, err = List(one.ID, 1, 20, "batch")
	if err != nil || page.Total != 125 {
		t.Fatal(page, err)
	}
	for _, args := range [][2]int{{-1, 20}, {1, 101}, {1000001, 20}} {
		if _, err := List(one.ID, args[0], args[1], ""); !errors.Is(err, ErrParams) {
			t.Fatal(args, err)
		}
	}
	if _, err := Resolve("owner@external.test"); !errors.Is(err, ErrRecipient) {
		t.Fatal("relay permitted", err)
	}
	if _, err := Resolve("owner+not-registered@example.com"); !errors.Is(err, ErrRecipient) {
		t.Fatal("implicit plus addressing", err)
	}
}
func TestAliasConcurrentReservationAgainstNewUser(t *testing.T) {
	engine, one, _, _ := aliasDB(t)
	for i := 0; i < 12; i++ {
		local := fmt.Sprintf("race%d", i)
		var wg sync.WaitGroup
		var aliasErr, userErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, aliasErr = Create(one.ID, local, "example.com", "") }()
		go func() { defer wg.Done(); userErr = InsertUser(&models.User{Account: local}) }()
		wg.Wait()
		if (aliasErr == nil) == (userErr == nil) {
			t.Fatalf("exactly one reservation must succeed: %v %v", aliasErr, userErr)
		}
	}
	// 唯一索引也必须阻止相同完整地址的直接重复写入。
	a, err := Create(one.ID, "unique", "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	a.ID = 0
	if _, err = engine.Insert(a); err == nil {
		t.Fatal("database permits duplicate aliases")
	}
}
func TestAliasDatabaseFailureIsNotUnknownRecipient(t *testing.T) {
	engine, _, _, _ := aliasDB(t)
	engine.Close()
	if _, err := Resolve("missing@example.com"); err == nil || errors.Is(err, ErrRecipient) {
		t.Fatal("database failure hidden", err)
	}
}
