package pop3_server

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	pcontext "github.com/Jinnrry/pmail/utils/context"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
	"xorm.io/xorm"
)

var listenerMu sync.Mutex
var listener net.Listener
var mailboxLocks sync.Map // 同账号只允许一个POP事务，退出时释放。

func StartWithTls() {
	crt, err := tls.LoadX509KeyPair(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
	if err != nil {
		panic(err)
	}
	ln, err := protocolsecurity.ListenTLS(config.Instance.POP3Bind, config.Instance.POP3Port, crt)
	if err != nil {
		panic(err)
	}
	listenerMu.Lock()
	listener = ln
	listenerMu.Unlock()
	if err = servePOP3(ln); err != nil && !errors.Is(err, net.ErrClosed) {
		panic(err)
	}
}
func Stop() {
	listenerMu.Lock()
	defer listenerMu.Unlock()
	if listener != nil {
		listener.Close()
		listener = nil
	}
}
func servePOP3(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handlePOP3(conn)
	}
}

type popItem struct {
	ID      int `xorm:"id"`
	EmailID int `xorm:"email_id"`
	Size    int
}

// 每条连接使用独立序号快照；数据库ID不作为客户端可猜测的邮件序号。
func handlePOP3(conn net.Conn) {
	defer conn.Close()
	if tc, ok := conn.(*tls.Conn); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := tc.HandshakeContext(ctx)
		cancel()
		if err != nil {
			return
		}
	}
	grant := protocolsecurity.NewSession("pop3", func() { conn.Close() })
	defer grant.Close()
	reader := bufio.NewReaderSize(conn, 4096)
	writer := bufio.NewWriter(conn)
	tp := textproto.NewWriter(writer)
	reply := func(ok bool, msg string) {
		prefix := "-ERR "
		if ok {
			prefix = "+OK "
		}
		fmt.Fprintf(writer, "%s%s\r\n", prefix, msg)
		writer.Flush()
	}
	multiline := func(value []byte) {
		reply(true, "message follows")
		dot := tp.DotWriter()
		dot.Write(value)
		dot.Close()
		writer.Flush()
	}
	reply(true, "PMail POP3 ready")
	var username string
	var user models.User
	var items []popItem
	deleted := map[int]bool{}
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return
		}
		conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		parts := strings.Fields(string(line))
		if len(parts) == 0 {
			reply(false, "invalid command")
			continue
		}
		cmd := strings.ToUpper(parts[0])
		arg := parts[1:]
		arity := map[string]int{"CAPA": 0, "QUIT": 0, "NOOP": 0, "RSET": 0, "STAT": 0, "USER": 1, "PASS": 1, "DELE": 1, "RETR": 1, "TOP": 2}
		if want, known := arity[cmd]; known && len(arg) != want {
			reply(false, "invalid arguments")
			continue
		}
		if (cmd == "LIST" || cmd == "UIDL") && len(arg) > 1 {
			reply(false, "invalid arguments")
			continue
		}

		if user.ID > 0 && grant.Check() != nil {
			reply(false, "authorization expired")
			return
		}
		switch cmd {
		case "CAPA":
			multiline([]byte("USER\r\nUIDL\r\nTOP\r\n"))
			continue
		case "QUIT":
			if user.ID > 0 && len(deleted) > 0 {
				err = db.Transaction(db.Instance, func(s *xorm.Session) error {
					for n := range deleted {
						_, err := s.ID(items[n-1].ID).Where("user_id=? AND status=0 AND group_id=0", user.ID).Cols("status", "group_id").Update(&models.UserEmail{Status: 3})
						if err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					reply(false, "update failed")
					return
				}
			}
			reply(true, "bye")
			return
		case "USER":
			if user.ID != 0 || len(arg) != 1 {
				reply(false, "invalid state")
				continue
			}
			username = arg[0]
			reply(true, "send access code")
			continue
		case "PASS":
			if user.ID != 0 || username == "" || len(arg) != 1 {
				reply(false, "invalid state")
				continue
			}
			u, err := grant.Login(username, arg[0])
			if err != nil {
				reply(false, "authentication failed")
				return
			}
			if _, busy := mailboxLocks.LoadOrStore(u.ID, true); busy {
				reply(false, "mailbox locked")
				return
			}
			defer mailboxLocks.Delete(u.ID)
			err = db.Instance.Table("user_email").Alias("ue").Select("ue.id,ue.email_id,e.size").Join("INNER", "email e", "e.id=ue.email_id").Where("ue.user_id=? AND ue.status=0 AND ue.group_id=0", u.ID).Asc("ue.id").Find(&items)
			if err != nil {
				reply(false, "mailbox unavailable")
				return
			}
			// 按实际重建MIME的octet数公布大小，与原入库大小分开。
			for i := range items {
				var e models.Email
				ok, err := db.Instance.ID(items[i].EmailID).Get(&e)
				if err != nil || !ok {
					reply(false, "mailbox unavailable")
					return
				}
				items[i].Size = len(parsemail.NewEmailFromModel(e).BuildBytes(&pcontext.Context{UserID: u.ID}, false))
			}
			user = u
			reply(true, "authenticated")
			continue
		}
		if user.ID == 0 {
			reply(false, "authenticate first")
			continue
		}
		if cmd == "NOOP" {
			reply(true, "ok")
			continue
		}
		if cmd == "RSET" {
			deleted = map[int]bool{}
			reply(true, "reset")
			continue
		}
		if cmd == "STAT" {
			count, size := 0, 0
			for i, item := range items {
				if !deleted[i+1] {
					count++
					size += item.Size
				}
			}
			reply(true, fmt.Sprintf("%d %d", count, size))
			continue
		}
		if (cmd == "LIST" || cmd == "UIDL") && len(arg) == 0 {
			var rows strings.Builder
			for i, item := range items {
				if deleted[i+1] {
					continue
				}
				if cmd == "LIST" {
					fmt.Fprintf(&rows, "%d %d\r\n", i+1, item.Size)
				} else {
					fmt.Fprintf(&rows, "%d u%d-e%d\r\n", i+1, item.ID, item.EmailID)
				}
			}
			multiline([]byte(rows.String()))
			continue
		}
		n := 0
		if len(arg) > 0 {
			n, _ = strconv.Atoi(arg[0])
		}
		if n < 1 || n > len(items) || deleted[n] {
			reply(false, "no such message")
			continue
		}
		item := items[n-1]
		// 复验快照中的关联仍属于当前用户，不能通过猜数据库ID访问其他账号。
		exists, err := db.Instance.ID(item.ID).Where("user_id=? AND email_id=? AND status=0 AND group_id=0", user.ID, item.EmailID).Exist(&models.UserEmail{})
		if err != nil || !exists {
			reply(false, "message unavailable")
			continue
		}
		switch cmd {
		case "LIST":
			reply(true, fmt.Sprintf("%d %d", n, item.Size))
		case "UIDL":
			reply(true, fmt.Sprintf("%d u%d-e%d", n, item.ID, item.EmailID))
		case "DELE":
			deleted[n] = true
			reply(true, "marked")
		case "RETR", "TOP":
			var model models.Email
			ok, err := db.Instance.ID(item.EmailID).Get(&model)
			if err != nil || !ok {
				reply(false, "message unavailable")
				continue
			}
			raw := parsemail.NewEmailFromModel(model).BuildBytes(&pcontext.Context{UserID: user.ID}, false)
			if cmd == "TOP" {
				if len(arg) != 2 {
					reply(false, "invalid TOP")
					continue
				}
				lines, err := strconv.Atoi(arg[1])
				if err != nil || lines < 0 {
					reply(false, "invalid TOP")
					continue
				}
				text := strings.SplitN(string(raw), "\r\n\r\n", 2)
				if len(text) == 2 {
					body := strings.Split(text[1], "\r\n")
					if lines < len(body) {
						body = body[:lines]
					}
					raw = []byte(text[0] + "\r\n\r\n" + strings.Join(body, "\r\n"))
				}
			}
			multiline(raw)
		default:
			reply(false, "unsupported command")
		}
	}
}
