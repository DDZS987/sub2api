# 交给 Claude 的迁移提示词

以下提示词按阶段使用。不要把所有阶段一次性交给 Claude，必须在每阶段验收后再继续。

## 阶段一：只读检查新机

```text
你现在负责 Sub2API 香港新 VPS 的迁移准备。先阅读 /home/ubuntu/sub2api/AGENTS.md 和 deploy/migration/README_CN.md。

重要边界：
1. 旧机仍承载生产服务和当前对话，禁止停止、重启、重载或重建旧机任何容器和系统服务。
2. 本阶段只允许只读检查新机，不安装软件、不修改防火墙、不创建数据库、不启动 Compose。
3. 不显示任何密码、令牌、订阅地址、Reality 私钥或 deploy/.env 内容。

请检查并汇报：新机操作系统和架构、CPU/内存/磁盘、公网 IPv4/IPv6 是否真实可用、Docker 与 Compose 是否安装、80/443/24443-24480 端口占用、时区、防火墙状态、DNS 出口和到旧机/对象存储的网络质量。确认新机是否为 amd64。最后给出风险清单，不执行修复。
```

## 阶段二：准备新机证书和目录

```text
你负责在香港新 VPS 上准备 Sub2API 的基础系统、Docker、证书和防火墙，但不得操作旧机服务。

开始前再次检查 hostname、外网 IPv4、uptime、free -h、df -h，并确认目标是新机 154.12.30.105。按 /home/ubuntu/sub2api/deploy/migration/README_CN.md 执行。

基础系统要求：
1. 通过 Docker 官方 Ubuntu APT 仓库安装 Docker Engine、containerd、Buildx 和 Compose 插件。安装过程可能自动启动新机 Docker daemon 和 containerd，这是本阶段仅允许启动的系统服务；不得启动任何项目容器。
2. 在安装 Docker 前先准备 `/etc/docker/daemon.json` 日志轮转，限制单个容器日志大小和保留数量，防止 40GB 磁盘被日志写满；写入后先做 JSON 语法检查。不要设置 `iptables=false`。
3. 当前使用 root 密钥登录，不要无条件创建 `ubuntu` 用户或把任何用户加入 `docker` 组。只有确认确实需要非 root 运维账号后另行评估；`docker` 组等同于高权限。
4. 创建 2GB Swap，不创建 4GB 或 8GB Swap；权限 600。所有步骤必须可重复执行：已有 swapfile 时不覆盖，`/etc/fstab` 已有相同条目时不重复追加。
5. 将 `vm.swappiness=10` 和 Redis 需要的 `vm.overcommit_memory=1` 写入独立的 `/etc/sysctl.d/99-sub2api.conf`，不要反复追加 `/etc/sysctl.conf`；`somaxconn=4096` 已足够，不做其他宽泛内核优化。
6. 时区改为 `Asia/Shanghai`。
7. 不修改当前 DNS 解析器。旧机 `10.3.0.8` 是私网地址，新机无法访问属于正常现象。

防火墙要求：
1. 启用 UFW 前先读取 SSH 实际端口并明确放行，保持当前 SSH 会话，再开第二条 SSH 会话验证，防止锁死。
2. 默认拒绝入站、允许出站；本阶段只明确放行 SSH、80 和 443。
3. 不在本阶段决定 24480 是否公开。24443-24472 是后续 Reality 公网端口，也等阶段三配置复核后再处理。
4. Docker 官方明确说明：Docker 发布的容器端口可能绕过 UFW。必须检查 Docker 的 iptables/DOCKER-USER 行为，不能把 UFW 状态当成容器端口已受保护的证据。
5. PostgreSQL、Redis 和 Sub2API 8080 最终只能处于 Docker 内网或绑定 127.0.0.1，绝不暴露公网。

证书要求：
1. 保留 Cloudflare 作为 DNS 托管，暂时不要改变现有 A/AAAA 记录和橙云状态。
2. 使用 Certbot 的 Cloudflare DNS 插件，通过 DNS-01 申请 Let's Encrypt 公网证书。
3. 只使用限制到 fyai.space 的 Zone:DNS:Edit API Token，禁止使用 Global API Key。
4. 凭据写入 /root/.secrets/certbot/cloudflare.ini，目录权限 700、文件权限 600，不在终端输出令牌。
5. 申请 fyai.space 和 *.fyai.space，cert-name 固定为 fyai.space。
6. Certbot 与 Cloudflare 插件必须使用同一包管理来源。Ubuntu 22.04 优先先检查并使用 `apt install certbot python3-certbot-dns-cloudflare`；禁止同时使用 APT Certbot 和 `sudo pip3 install certbot-dns-cloudflare`。安装后用 `certbot plugins` 确认 `dns-cloudflare` 可见。
7. 验证证书链、有效期和 SAN。Nginx 容器尚不存在，本阶段不要创建会无条件重载容器的续期钩子；续期钩子留到阶段三，或必须先判断 `sub2api-nginx` 正在运行才重载。
8. 如果 API Token 尚未准备好，完成 Docker、Swap、时区和防火墙准备后停下，提示我在 Cloudflare 创建令牌；不要要求我在聊天中粘贴令牌。

安装或修改前先列出拟执行命令、软件来源、磁盘占用和影响，等我确认后再执行。完成后输出 Docker/Compose 版本、Swap、时区、UFW、Docker 防火墙行为、证书路径与有效期的证据。不要启动 Sub2API、PostgreSQL、Redis、sing-box 或 Nginx，不要修改 Cloudflare A/AAAA/NS 记录。
```

## 阶段三：新机空数据预演

```text
你负责在香港新 VPS 上做 Sub2API 空数据预演。旧机生产服务不得有任何启停、重载或配置变更。

目标机代码目录固定为 `/opt/sub2api`。使用：
docker compose -f deploy/docker-compose.yml -f deploy/migration/docker-compose.rehearsal.yml

要求：
1. 先确认已有一份旧机 PostgreSQL 远端备份成功且可下载，作为所有数据库变更前的保护点。
2. 目标机只使用临时空数据库进行启动检查，不导入生产账号数据。
3. 使用旧机导出的 weishaw/sub2api:local 镜像，不在旧机或新机执行高资源源码构建。
4. deploy/.env、证书凭据和 VLESS 私钥不得进入 Git或终端输出。
5. 先运行 docker compose config 验证，确认 Redis 最终命令包含 appendonly yes；服务只能有 postgres、redis、sub2api、nginx；Nginx 80/443 必须只绑定 127.0.0.1，不能绑定公网。
6. 启动前列出将启动的目标机容器，等我确认后再执行。
7. 在新机本地使用 `curl --resolve fyai.space:443:127.0.0.1` 检查 HTTPS、/health、Nginx 注册接口封锁、容器健康和磁盘占用；禁止使用公网 IP 进行预演访问。
8. 预演完成后是否停止目标机容器也必须先征得我确认。

不要触碰 Cloudflare DNS，不要让目标机接收真实用户流量。
```

## 阶段四：最终切换

```text
你负责执行 Sub2API 最终迁移。当前对话依赖旧服务，所以开始前必须先复述完整步骤、回滚点和预计中断时间，确认你能在对话断开后独立完成。未经我明确回复“开始最终切换”，不得执行任何启停或数据库写入。

范围只有：Sub2API、PostgreSQL、Redis、应用数据卷、sing-box 与其订阅文件、目标机 Nginx。不得操作其他项目。

强制顺序：
1. 检查旧机和新机资源、时间、目标地址、最新远端备份状态。
2. 禁止新请求进入旧 Sub2API，并等待现有请求结束；给出真实连接和日志证据。
3. 只停止旧机 sub2api 应用容器，保持旧 PostgreSQL、Redis、sing-box 和 Nginx 运行。
4. 在旧 Redis 执行同步 SAVE，验证 dump.rdb 时间和大小。
5. 从旧机后台创建最终 Sub2API 内置 PostgreSQL 备份，等待 completed，验证远端文件大小和可下载性。
6. 安全传输最终数据库文件、Redis 快照、deploy/.env、应用数据卷有效文件和当前 sing-box/VLESS 文件。
7. 目标机恢复 PostgreSQL 前确认备份存在、校验通过并写出回滚命令；恢复期间目标机 Sub2API 必须停止。
8. 恢复 Redis 后确认键数量合理、appendonly 已开启；确认 PostgreSQL 表数量、关键记录数量和迁移版本。
9. 设置新机 VLESS_RELAY_PUBLIC_HOST，保持原 Reality 密钥、订阅文件名和端口映射。最终切换前不要启用新机同步定时器。
10. 启动目标机服务前再次向我列出容器和端口，得到确认后执行。
11. 验证 HTTPS、登录、账号数量、调度缓存、模型列表和一条低风险真实请求。
12. 将旧 Nginx 临时改为转发到新机，使仍走 Cloudflare 旧路径的请求也只进入新机。
13. 在 Cloudflare 将 fyai.space 和 api.fyai.space 改为新机 IPv4 并设为 DNS only。只有在新机 IPv6 已实际验证可用时才设置 AAAA；否则删除或不创建源站 AAAA。不要修改域名注册商和 NS。
14. 停止旧机 VLESS 同步定时器，但旧 sing-box 和旧订阅入口保留过渡；让旧订阅入口返回新机节点。
15. 持续观察错误率、数据库连接、Redis、磁盘、证书和真实请求。未经确认不删除旧机容器、数据卷、镜像或备份。

回滚限制：目标机一旦接收真实写入，不能直接启动旧应用接受流量，否则数据库会分叉。若切换后回滚，必须先把目标机最新数据库安全回传并恢复到旧机。
```
