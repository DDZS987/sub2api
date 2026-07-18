# Sub2API 香港 VPS 迁移前置说明

本文档只描述迁移准备和交接步骤。当前生产机上的容器、数据库、定时器和 Nginx 不应由 Codex 启停；涉及启停、重载、数据库恢复和 DNS 切换的动作统一交给 Claude 执行。

## 已确认的现状

- `fyai.space` 的注册商虽然是 Porkbun，但当前权威 DNS 位于 Cloudflare。
- `fyai.space` 和 `api.fyai.space` 当前使用 Cloudflare 橙云代理。
- Sub2API 内置 PostgreSQL 备份每天 `03:00` 执行，保留 7 份，最近一次已成功完成。
- 内置备份只包含 PostgreSQL，不包含 Redis、`sub2api_data` 数据卷和 `.dev` 下的 sing-box 文件。
- `sub2api-vless-relay-sync.timer` 当前仍处于启用和运行状态，每 5 分钟检查一次上游订阅。它不是数据库备份脚本，不能因为启用了 Sub2API 内置备份就直接删除。
- 当前生产镜像为 `amd64/linux`。目标机必须先确认也是 `amd64`，才能直接使用 `docker save` / `docker load` 搬运镜像。

## 迁移内容

需要迁移：

1. 当前 `weishaw/sub2api:local` 生产镜像。
2. Git 本地生产提交和部署文件。
3. `deploy/.env`，必须安全传输，不得提交到 Git。
4. Sub2API 内置备份产生的最新 PostgreSQL 压缩文件。
5. Redis 的 `/data/dump.rdb`。
6. `deploy_sub2api_data` 中除历史日志外的配置与价格文件。
7. `.dev/sing-box` 和 `.dev/vless-relay` 中当前有效文件。
8. Reality 密钥、上游订阅地址和新的公开地址配置。

不迁移：

- 旧机 `/var/lib/docker` 整体目录。
- Docker 构建缓存、废弃镜像和无关数据卷。
- 旧机 `.git` 的完整 2GB 历史对象；使用 Git bundle 或补丁保留本地提交。
- 历史运行日志和 `.bak.*` 订阅备份。
- 旧机整套 Nginx 配置；其中还包含其他站点和敏感配置。

## PostgreSQL 内置备份的正确用法

旧机通过后台“备份管理”创建最终备份，并等待状态变成“已完成”。新机第一次恢复时不能依赖后台页面，因为空数据库里还没有备份记录和远端存储配置。

正确顺序是：

1. 在旧机后台创建最终备份。
2. 从旧机后台取得该备份的临时下载地址。
3. 将压缩文件下载到新机的受保护目录。
4. 由 Claude 在已经备份确认无误后，将文件恢复到新机的空 PostgreSQL。
5. 恢复完成后，新数据库会同时带回远端存储设置、备份计划和备份记录。

内置备份使用 `pg_dump --clean --if-exists`，恢复时应使用单事务方式。恢复期间不能启动目标机 Sub2API，避免应用自动迁移或后台任务与恢复并发。

## Redis 前置修正

旧 Compose 的多行命令没有把持久化参数真正传给 Redis，运行中的 Redis 实际为 `appendonly no`。修正只放在目标机专用的 `docker-compose.target.yml`，基础生产 Compose 保持原样，当前生产 Redis 不进行重建。

最终迁移时需要：

1. 先停止旧机 Sub2API 写入。
2. 对旧 Redis 执行一次同步保存。
3. 确认 `/data/dump.rdb` 的修改时间和大小更新。
4. 将快照放入新机 Redis 数据卷。
5. 新机 Redis 启动后确认追加持久化已经开启，再启动 Sub2API。

## sing-box 与订阅

同步脚本当前仍在使用。新机使用 `vless-relay.env.example` 生成独立的 `/etc/sub2api-vless-relay.env`，不要继续依赖脚本内的旧 IP 默认值。

必须保留：

- `relay-state.json`，用于保持端口对应关系和原订阅文件名。
- `relay-meta.json`。
- 当前订阅 YAML 文件。
- `config.json`、`proxy-records.json`、`subscription.last.raw` 和 `subscription.last.meta.yaml`。
- 原 Reality 密钥。

目标机定时器在最终切换前不得启用，因为同步脚本检测到变化时会强制重建 sing-box 和订阅容器。

## 证书方案

推荐保留 Cloudflare 作为 DNS 托管商，只把记录改为“仅 DNS”，不迁回 Porkbun DNS。证书使用 Let's Encrypt，通过 Cloudflare DNS-01 验证提前签发：

- Cloudflare 令牌只授予 `fyai.space` 这一个区域的 `DNS:Edit` 权限。
- 禁止使用 Cloudflare Global API Key。
- 凭据文件保存为 `/root/.secrets/certbot/cloudflare.ini`，权限必须为 `600`。
- 证书申请 `fyai.space` 和 `*.fyai.space`，证书名称固定为 `fyai.space`。
- Nginx 只读挂载 `/etc/letsencrypt`。
- 自动续期成功后由部署钩子重载目标机的 `sub2api-nginx` 容器。

DNS-01 不要求域名先指向新机，因此可以在旧服务正常运行时完成证书签发。

官方参考：

- [Cloudflare 代理状态](https://developers.cloudflare.com/dns/proxy-status/)
- [Cloudflare 创建最小权限 API Token](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)
- [Certbot Cloudflare DNS 插件](https://certbot-dns-cloudflare.readthedocs.io/en/stable/)
- [Let's Encrypt DNS-01 验证说明](https://letsencrypt.org/docs/challenge-types/)

## 目标机 Compose

新机只使用以下组合，不加载旧机本地的 `docker-compose.override.yml`：

```bash
docker compose \
  -f deploy/docker-compose.yml \
  -f deploy/migration/docker-compose.target.yml \
  config
```

该命令只是渲染检查。真正的 `up`、`stop`、`restart` 和数据库恢复必须由 Claude 按 `CLAUDE_HANDOFF_CN.md` 执行。

## 目标机基础资源

- 新机 5.8GB 内存足够当前迁移范围，使用 2GB Swap 作为 OOM 缓冲即可；40GB 磁盘不分配 8GB Swap。
- Docker 安装前配置日志轮转，避免容器日志长期占满磁盘。
- 时区调整为 `Asia/Shanghai`，保留系统时间同步。
- 保留服务商当前 DNS 解析器，不在迁移期间增加无关网络变量。
- `10.3.0.8` 是旧机私网地址，新机无法访问正常；数据库通过远端备份恢复，其他小文件通过公网 SSH 传输。

## 直连安全边界

- 公网开放 80、443、SSH 和实际使用的 Reality 端口。
- 8080、5432、6379 只留在本机或 Docker 网络，不得暴露公网。
- 24480 订阅端口应优先限制来源；若需要公开订阅，后续改为 HTTPS 独立域名。
- Docker 发布端口可能绕过 UFW，必须同时审查 Compose 绑定地址和 Docker 的 `DOCKER-USER` 防火墙链。
- DNS 切换前先确认目标机是否真的具备可用 IPv6；没有 IPv6 时不得保留或新建指向错误地址的 AAAA 记录。
- 直连后不再有 Cloudflare WAF 和流量缓冲，必须保留 Nginx 的注册接口封锁、登录限速和连接数限制。
- 不信任客户端自行提供的 `CF-Connecting-IP`，直连来源地址以 Nginx 的 `$remote_addr` 为准。

## 对话连续性

当前对话依赖旧 Sub2API。最终切换前必须先把完整操作提示词交给 Claude，并确认 Claude 已读取、复述回滚点且能独立继续。Codex 不参与旧机或新机的服务启停。
