package http_server

import (
	"encoding/json"
	"fmt"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/mailaddress"
	"testing"
)

// 别名必须由当前网页登录用户创建，不能成为另一个可登录账号。
func TestEmailAliasSelfService(t *testing.T) {
	f := newCodeFixture(t, true)
	data := f.success(f.call("POST", "/api/settings/aliases/create", `{"local_part":" Shopping ","domain":"EXAMPLE.COM","remark":"测试"}`, "", f.cookie, true))
	var alias struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
	}
	if err := json.Unmarshal(data, &alias); err != nil || alias.ID == 0 || alias.Address != "shopping@example.com" {
		t.Fatalf("invalid alias: %s", data)
	}
	f.success(f.call("POST", "/api/settings/aliases/list", `{}`, "", f.cookie, true))
	w := f.call("POST", "/api/login", `{"account":"shopping@example.com","password":"test-password"}`, "", nil, true)
	var login struct {
		ErrorNo int `json:"errorNo"`
	}
	if json.Unmarshal(w.Body.Bytes(), &login) != nil || login.ErrorNo == 0 {
		t.Fatalf("alias logged in: %s", w.Body.String())
	}
	for _, body := range []string{`{"local_part":"shopping","domain":"example.com"}`, `{"local_part":"owner","domain":"example.com"}`, `{"local_part":"forged","domain":"example.com","user_id":2}`} {
		w := f.call("POST", "/api/settings/aliases/create", body, "", f.cookie, true)
		var result struct {
			ErrorNo int `json:"errorNo"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ErrorNo == 0 {
			t.Fatalf("accepted invalid creation: %s", w.Body.String())
		}
	}
	token, _ := f.create()
	for _, action := range []string{"create", "list", "update", "delete"} {
		mustDeny(t, f.call("POST", "/api/settings/aliases/"+action, `{}`, token, f.cookie, true))
	}
	mustDeny(t, f.call("POST", "/api/settings/aliases/create", `{}`, "", f.cookie, false))
	f.success(f.call("POST", "/api/settings/aliases/delete", fmt.Sprintf(`{"id":%d}`, alias.ID), "", f.cookie, true))
}

func TestEmailAliasOrdinaryUserAndCrossOwnerDenial(t *testing.T) {
	f := newCodeFixture(t, true)
	// 复用已有会话，改变数据库角色，避免测试本身耗尽生产登录限流桶。
	if _, err := f.engine.ID(f.owner.ID).Cols("is_admin").Update(&models.User{IsAdmin: 0}); err != nil {
		t.Fatal(err)
	}
	adminAlias, err := mailaddress.Create(f.owner.ID+1, "admin-only", "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	ordinary := f.cookie
	f.success(f.call("POST", "/api/settings/aliases/create", `{"local_part":"ordinary","domain":"example.com","remark":"自己的别名"}`, "", ordinary, true))
	var page struct {
		List []struct {
			ID      int64  `json:"id"`
			Address string `json:"address"`
		}
		Total int64
	}
	json.Unmarshal(f.success(f.call("POST", "/api/settings/aliases/list", `{}`, "", ordinary, true)), &page)
	if page.Total != 1 || len(page.List) != 1 || page.List[0].Address != "ordinary@example.com" {
		t.Fatalf("wrong ownership: %+v", page)
	}
	for _, tc := range []struct{ action, body string }{
		{"update", fmt.Sprintf(`{"id":%d,"remark":"stolen","enabled":true}`, adminAlias.ID)},
		{"delete", fmt.Sprintf(`{"id":%d}`, adminAlias.ID)},
		{"list", `{"user_id":1}`}, {"list", `{"page_size":101}`},
		{"create", `{"local_part":"x","domain":"external.test"}`},
		{"update", fmt.Sprintf(`{"id":%d,"remark":"omitted enabled"}`, page.List[0].ID)},
	} {
		w := f.call("POST", "/api/settings/aliases/"+tc.action, tc.body, "", ordinary, true)
		var result struct {
			ErrorNo int `json:"errorNo"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ErrorNo == 0 {
			t.Fatalf("accepted invalid request: %s", w.Body.String())
		}
	}
	f.success(f.call("POST", "/api/settings/aliases/update", fmt.Sprintf(`{"id":%d,"remark":"暂停","enabled":false}`, page.List[0].ID), "", ordinary, true))
	f.success(f.call("POST", "/api/settings/aliases/update", fmt.Sprintf(`{"id":%d,"remark":"恢复","enabled":true}`, page.List[0].ID), "", ordinary, true))
	// 即使当前用户被提升为管理员，也不能操作其他人的别名。
	if _, err := f.engine.ID(f.owner.ID).Cols("is_admin").Update(&models.User{IsAdmin: 1}); err != nil {
		t.Fatal(err)
	}
	w := f.call("POST", "/api/settings/aliases/delete", fmt.Sprintf(`{"id":%d}`, adminAlias.ID), "", f.cookie, true)
	var result struct {
		ErrorNo int `json:"errorNo"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.ErrorNo == 0 {
		t.Fatal("administrator bypassed alias ownership")
	}
	// 从HTTP账号创建入口反向阻止占用已有别名前缀。
	w = f.call("POST", "/api/user/create", `{"account":"ordinary","username":"User","password":"synthetic"}`, "", f.cookie, true)
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.ErrorNo == 0 {
		t.Fatal("new account stole alias")
	}
}

func TestAliasDetailHTTPReturnsOnlyOwnDeliveryAddresses(t *testing.T) {
	f := newCodeFixture(t, true)
	if _, err := f.engine.Where("email_id=? AND user_id=?", f.ownID, f.owner.ID).Cols("delivery_addresses").Update(&models.UserEmail{DeliveryAddresses: `["shopping@example.com"]`}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.engine.Insert(&models.UserEmail{UserID: f.owner.ID + 1, EmailID: f.ownID, DeliveryAddresses: `["hidden@example.com"]`}); err != nil {
		t.Fatal(err)
	}
	token, _ := f.create()
	data := f.success(f.call("POST", "/api/email/detail", fmt.Sprintf(`{"id":%d}`, f.ownID), token, nil, false))
	var detail struct {
		Addresses []string `json:"delivery_addresses"`
		Text      string   `json:"text"`
	}
	if err := json.Unmarshal(data, &detail); err != nil || len(detail.Addresses) != 1 || detail.Addresses[0] != "shopping@example.com" || detail.Text != "synthetic body" {
		t.Fatalf("missing or leaked delivery metadata: %s (%v)", data, err)
	}
}
