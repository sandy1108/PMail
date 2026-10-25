# IMAP、POP3与SMTP客户端

本分支使用统一客户端授权码。网页登录后在「设置 → 安全 → 客户端授权码」创建码，按需勾选IMAP、POP3和SMTP，可组合使用。客户端用户名必须是实际账号的完整邮箱地址（如`owner@example.com`），密码填写授权码；网页登录密码、接收别名和未授权用途不能登录。

## 监听和证书

三个客户端服务默认关闭。以下配置为回环隐式TLS示例，修改配置后需重启生效：

```json
{
  "enableSubmission": true,
  "submissionBind": "127.0.0.1",
  "submissionPort": 1465,
  "clientHost": "mail.example.com",
  "clientIMAPPort": 993,
  "clientPOP3Port": 995,
  "clientSMTPPort": 465,
  "enableIMAP": true,
  "imapBind": "127.0.0.1",
  "imapPort": 1993,
  "enablePOP3": true,
  "pop3Bind": "127.0.0.1",
  "pop3Port": 1995,
  "sslType": "1",
  "SSLPublicKeyPath": "config/ssl/fullchain.pem",
  "SSLPrivateKeyPath": "config/ssl/private.key"
}
```

- 只监听配置的TLS端口，不启动110/143/587，不支持STLS/STARTTLS升级。最低TLS1.2，客户端选「SSL/TLS」，不要选「STARTTLS」。
- 内部端口要求1024–65535。外部可用TCP转发映射常用的IMAPS 993、POP3S 995、SMTPS 465到内部1993、1995、1465；也可使用明确配置的其他外部端口。客户端填外部可达端口。
- 证书文件必须是客户端连接主机名（如`mail.example.com`）的有效服务器证书和完整链，私钥仅服务账号可读。可以与HTTPS共用同域名证书，但PMail必须能读取自己的证书文件；仅更新Nginx证书不会更新PMail。更新后受控重启并分别验证。
- 客户端验证证书主机名和信任链，不能关闭验证来绕过配置问题。回环自签证书只用于隔离测试。
- 反向代理应按原始TCP透传TLS，不使用HTTP代理规则。当前IMAP/POP3/SMTP提交入口不接受PROXY头，不要给这些转发开启PROXY protocol。入站SMTP25的PROXY设置独立。
- 默认回环监听意味着局域网和公网不能直接访问。授权码用途选择不会开启监听或自动改防火墙、FRP。

## 安全边界

每条命令重新校验授权码，闲置连接每秒复验。撤销、到期、禁用用户、修改网页登录密码或授权用途后，已有协议连接将在下一命令或约一秒内关闭；重新连接使用同一个码和当前用途。已经完成的下载不能追回，已通过验证的在途操作可能完成。

每个TLS监听最多32个TCP连接，握手期限10秒，读空闲上限5分钟，写期限30秒。协议登录共用有界全局桶，初始10次、每3秒补1次；因此短时间内大量合法重连也会失败，稍候重试。没有IP白名单或封禁清单的无限内存增长。协议不记录原始认证命令或授权码。

## 支持行为

- POP3：仅收件箱；USER/PASS、STAT、LIST、UIDL、RETR、TOP、DELE、RSET、QUIT、NOOP、CAPA。序号从1开始，UIDL绑定邮件关系ID，不能拿数据库正文ID访问。每个账号同时一个POP3事务。DELE仅在本连接标记，RSET/断线取消；正常QUIT把邮件移到已删除文件夹。建议客户端启用「在服务器保留副本」。
- IMAP：登录、目录列表、选择/只读选择、状态、搜索、读取、已读和删除标记、复制/移动、EXPUNGE、APPEND、目录管理及IDLE新邮件通知。EXAMINE读取不修改已读状态。EXPUNGE永久移除当前邮箱关系，不影响其他账号及副本；不自动清理无引用正文。
- APPEND上限25MiB，仅导入当前用户邮箱，可含MIME附件，不通过SMTP发送邮件。当前永久邮件标记支持`\Seen`和`\Deleted`；其他标记会明确报不支持。订阅/取消订阅沿用兼容响应，不持久化订阅状态。
- IMAP/POP3不用于发送邮件。SMTP提交须显式启用enableSubmission并勾选SMTP用途码，见下节。

## 升级、验证与回退

启动时数据库增量新增授权用途、权限版本及邮件关系删除标记；旧授权记录空用途仍仅HTTP只读，不自动获得协议权限。升级前备份数据库及配置，保留旧二进制；回退旧版本前先关闭IMAP/POP3，以免旧服务恢复旧密码认证方式。多用途及经编辑的码使用旧程序不识别的scope，旧版本会拒绝这些码；回退后需重新签发旧版只读码，不要手工把scope改回去。

隔离测试使用临时SQLite、动态测试证书、回环随机端口和合成账号：

```sh
cd server
go test ./config ./services/accesscode ./services/protocolsecurity ./listen/http_server ./listen/imap_server ./listen/pop3_server -count=1
```

当前验证基于SQLite和Go客户端。其他数据库、真实桌面客户端、多网络穿透、长时间并发和资源占用需要部署前后另行验收，不能用本地构建替代。SQLite UIDNEXT使用持久自增高水位；其他数据库仍需补充对应的高水位实现和回归后才能视为完整兼容。


## SMTP提交和页面教程

设置→安全→客户端授权码旁的“如何配置邮件客户端”会显示实际配置的服务器、完整邮箱用户名、对外端口及服务启用状态。推荐IMAP收信＋SMTP发信，勾选同一授权码的IMAP和SMTP用途。POP3可另选。SMTP发送服务器开启认证，用户名填真实完整邮箱，密码填授权码；安全类型SSL/TLS。

提交端口仅隐式TLS，支持AUTH PLAIN/LOGIN（均在TLS内），不支持STARTTLS。必须验证授权码生命周期，不可使用网页登录密码、AI用途码、收件别名登录或借管理员身份冒用其他账号发信；发件信封及From/Sender均约束到真实账号。收件人数最多50、单封30MiB，入站25与客户端465独立，25不开放AUTH和外域中继。

实际投递以RCPT信封收件人为准，而非从信头添加收件人；邮件及结果沿用审计存储。外发网络、PTR、SPF、DKIM与DMARC的配置及送达率仍需独立验收，客户端TLS不代表服务器到最终收件人的全程加密保证。入站25保留机会式STARTTLS。

如果服务器本机hosts把主机名指向回环地址，客户端应填对应本机端口，而不是FRP对外端口。教程给出本机端口提示。证书主机名与信任链仍必须验证，不用IP地址绕过证书错误。
