// Package mailaddress统一收件地址解析和地址占用检查；不用于认证。
package mailaddress

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"xorm.io/xorm"
)

var (
	ErrParams    = errors.New("invalid alias parameters")
	ErrConflict  = errors.New("address already reserved")
	ErrNotFound  = errors.New("alias unavailable")
	ErrRecipient = errors.New("recipient unavailable")
	namespaceMu  sync.Mutex
	localPattern = regexp.MustCompile(`^[a-z0-9_+\-]+(?:\.[a-z0-9_+\-]+)*$`)
)

// WithWrite串行化单进程中的地址变更及最终投递复验。禁止在回调中再次调用它。
// 多进程共享数据库须另行提供数据库级地址命名空间约束。
func WithWrite(fn func() error) error {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	return fn()
}
func IsLocalDomain(domain string) bool {
	if config.Instance == nil {
		return false
	}
	if strings.EqualFold(domain, config.Instance.Domain) {
		return true
	}
	for _, d := range config.Instance.Domains {
		if strings.EqualFold(domain, d) {
			return true
		}
	}
	return false
}
func normalize(local, domain string) (string, string, error) {
	local, domain = strings.ToLower(strings.TrimSpace(local)), strings.ToLower(strings.TrimSpace(domain))
	if len(local) == 0 || len(local) > 64 || !localPattern.MatchString(local) || domain == "" || !IsLocalDomain(domain) || len(local)+len(domain)+1 > 254 {
		return "", "", ErrParams
	}
	return local, domain, nil
}

// Recipient保存RCPT接受时的归属快照，防止DATA阶段因删除重建发生错投。
type Recipient struct {
	Address string
	User    models.User
	AliasID int64
}

func Resolve(address string) (Recipient, error) {
	s := db.Instance.NewSession()
	defer s.Close()
	return ResolveIn(s, address)
}
func ResolveIn(s *xorm.Session, address string) (Recipient, error) {
	parts := strings.Split(address, "@")
	if len(parts) != 2 {
		return Recipient{}, ErrRecipient
	}
	local, domain, err := normalize(parts[0], parts[1])
	if err != nil {
		return Recipient{}, ErrRecipient
	}
	r := Recipient{Address: local + "@" + domain}
	var users []models.User
	if err = s.Where("LOWER(account)=?", local).Limit(2).Find(&users); err != nil {
		return r, err
	}
	if len(users) > 1 {
		return r, errors.New("ambiguous account namespace")
	}
	if len(users) == 1 {
		if users[0].Disabled != 0 {
			return r, ErrRecipient
		}
		r.User = users[0]
		return r, nil
	}
	var alias models.EmailAlias
	found, err := s.Where("address=?", r.Address).Get(&alias)
	if err != nil {
		return r, err
	}
	if !found || !alias.Enabled {
		return r, ErrRecipient
	}
	found, err = s.Where("id=? AND disabled=0", alias.UserID).Get(&r.User)
	if err != nil {
		return r, err
	}
	if !found {
		return r, ErrRecipient
	}
	r.AliasID = alias.ID
	return r, nil
}

// InsertUser与别名创建共用命名空间锁及事务，防止并发检查后互相占用。
func InsertUser(user *models.User) error {
	local := strings.ToLower(strings.TrimSpace(user.Account))
	if len(local) == 0 || len(local) > 20 || !localPattern.MatchString(local) {
		return ErrParams
	}
	user.Account = local
	return WithWrite(func() error {
		return db.Transaction(db.Instance, func(s *xorm.Session) error {
			count, err := s.Table(&models.User{}).Where("LOWER(account)=?", local).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			count, err = s.Table(&models.EmailAlias{}).Where("local_part=?", local).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			user.ID = 0
			_, err = s.Insert(user)
			return err
		})
	})
}
func validRemark(remark string) bool {
	return utf8.ValidString(remark) && utf8.RuneCountInString(remark) <= 200
}
func Create(userID int, local, domain, remark string) (*models.EmailAlias, error) {
	local, domain, err := normalize(local, domain)
	if err != nil || userID <= 0 || !validRemark(remark) {
		return nil, ErrParams
	}
	alias := &models.EmailAlias{UserID: userID, LocalPart: local, Domain: domain, Address: local + "@" + domain, Remark: strings.TrimSpace(remark), Enabled: true}
	err = WithWrite(func() error {
		return db.Transaction(db.Instance, func(s *xorm.Session) error {
			var user models.User
			ok, err := s.Where("id=? AND disabled=0", userID).Get(&user)
			if err != nil {
				return err
			}
			if !ok {
				return ErrNotFound
			}
			count, err := s.Table(&models.User{}).Where("LOWER(account)=?", local).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			count, err = s.Table(&models.EmailAlias{}).Where("address=?", alias.Address).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			alias.ID = 0
			_, err = s.Insert(alias)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return alias, nil
}

type Page struct {
	List        []models.EmailAlias `json:"list"`
	Total       int64               `json:"total"`
	CurrentPage int                 `json:"current_page"`
	PageSize    int                 `json:"page_size"`
}

func List(userID, page, size int, keyword string) (*Page, error) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if userID <= 0 || page < 1 || page > 1000000 || size < 1 || size > 100 || len(keyword) > 200 {
		return nil, ErrParams
	}
	query := func() *xorm.Session {
		s := db.Instance.Where("user_id=?", userID)
		if keyword != "" {
			s.And("(address LIKE ? OR remark LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
		}
		return s
	}
	result := &Page{List: []models.EmailAlias{}, CurrentPage: page, PageSize: size}
	s := query()
	count, err := s.Count(&models.EmailAlias{})
	s.Close()
	if err != nil {
		return nil, err
	}
	result.Total = count
	s = query()
	defer s.Close()
	err = s.Desc("id").Limit(size, (page-1)*size).Find(&result.List)
	return result, err
}
func Update(userID int, id int64, remark string, enabled bool) error {
	if userID <= 0 || id <= 0 || !validRemark(remark) {
		return ErrParams
	}
	return WithWrite(func() error {
		n, err := db.Instance.Where("id=? AND user_id=?", id, userID).Cols("remark", "enabled").Update(&models.EmailAlias{Remark: strings.TrimSpace(remark), Enabled: enabled})
		if err != nil {
			return err
		}
		if n == 0 {
			// 部分数据库对重复提交相同值返回0，不能据此误判记录不存在。
			exists, lookupErr := db.Instance.Where("id=? AND user_id=?", id, userID).Exist(&models.EmailAlias{})
			if lookupErr != nil {
				return lookupErr
			}
			if !exists {
				return ErrNotFound
			}
		}
		return nil
	})
}
func Delete(userID int, id int64) error {
	if userID <= 0 || id <= 0 {
		return ErrParams
	}
	return WithWrite(func() error {
		n, err := db.Instance.Where("id=? AND user_id=?", id, userID).Delete(&models.EmailAlias{})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
