# Sub2API 项目级运行与发布规则

本文件是本机 `/opt/sub2api` 工作区的项目级说明。除非用户明确要求，不要
停止、重启、删除或重建生产服务。

## 1. 路径和环境

### 源码仓库

- 源码根目录：`/opt/sub2api`
- Git 远端：`origin=https://github.com/DDZS987/sub2api.git`
- 官方远端：`upstream=https://github.com/Wei-Shaw/sub2api.git`
- 当前本地 `main` 包含本地生产定制，不等同于 fork 的 `main`。
- 不要在未提交的工作区上直接发布生产。

### 开发环境

开发环境控制文件在 `/root/sub2api-dev`，不属于生产 Compose 项目：

- Compose：`/root/sub2api-dev/compose.yaml`
- 后端环境：`/root/sub2api-dev/backend.env`（权限应为 `600`）
- PostgreSQL：`127.0.0.1:15432`，Docker 卷 `sub2api-dev_postgres_data`
- Redis：`127.0.0.1:16379`，Docker 卷 `sub2api-dev_redis_data`
- 后端：`http://127.0.0.1:18080`
- 前端：`http://127.0.0.1:13000`

常用命令：

```bash
sub2api-dev-infra up
sub2api-dev-backend       # 单独终端，源码 go run
sub2api-dev-frontend      # 单独终端，Vite 开发服务器
sub2api-dev-infra status
sub2api-dev-infra down
```

不要使用生产的 `deploy_*` 卷作为开发数据库，也不要使用
`deploy/docker-compose.dev.yml` 代替上述开发环境；后者默认占用生产使用的
8080 端口并且会混淆容器生命周期。

### 生产环境

生产 Docker Compose 项目为 `deploy`：

- Compose 工作目录：`/opt/sub2api/deploy`
- 当前 Compose 配置：
  - `/opt/sub2api/deploy/docker-compose.yml`
  - `/opt/sub2api/deploy/migration/docker-compose.target.yml`
- 应用容器：`sub2api`
- 当前应用镜像：`weishaw/sub2api:local`
- 应用入口：宿主机 `127.0.0.1:8080`
- 反向代理：`sub2api-nginx`，对外使用 80/443
- PostgreSQL 容器：`sub2api-postgres`
- Redis 容器：`sub2api-redis`
- 其他生产容器：`sub2api-sing-box-vless`、`sub2api-vless-relay-sub`

主要生产数据卷：

- `deploy_sub2api_data`：应用配置、价格文件和运行数据
- `deploy_postgres_data`：生产 PostgreSQL 数据
- `deploy_redis_data`：生产 Redis 数据
- `deploy_nginx_logs`：Nginx 日志

生产密钥和运行配置位于 `/opt/sub2api/deploy/.env`，不得读取后输出、不得
提交 Git、不得复制到开发环境。

## 2. 开发反馈速度和构建规则

开发和生产不是同一个运行实例，也不是完全相同的运行模式：

- 开发后端直接从源码运行，`go run ./cmd/server`；改 Go 代码后需要重启
  `sub2api-dev-backend`，Go 会复用缓存并增量编译，不需要重建 Docker 镜像。
- 当前后端没有自动文件监视器；不要把 `go run` 称为真正的自动热重载。
- 开发前端使用 Vite，改 Vue/TS/CSS 通常通过 HMR 立即生效，不需要完整构建。
- `pnpm run build` 会生成 `backend/internal/web/dist`，但不会自动更新生产容器。
- 生产镜像把前端构建产物和 Go 后端打包在镜像中，生产应用代码变更必须重新
  构建镜像并重新创建 `sub2api` 容器。
- 开发和生产的 PostgreSQL/Redis 大版本目前均为 PostgreSQL 18、Redis 8，
  但数据库、端口、凭据、卷、日志、模式均严格隔离。
- Dockerfile 的前端构建环境与本机开发 Node 版本不必完全相同；正式发布以
  Dockerfile 和 CI 的 Go/pnpm 检查为准。

提交前建议至少运行：

```bash
cd /opt/sub2api/frontend
pnpm run typecheck
pnpm run lint:check
pnpm run build

cd /opt/sub2api/backend
go test -tags=unit ./...
```

## 3. 将代码安全应用到生产

发布必须从已提交、可追溯的 Git 提交进行。不要在生产服务器执行普通
`git pull`，也不要直接把工作区未提交修改构建成生产镜像。

### 发布前检查

```bash
cd /opt/sub2api
git status --short --branch
git log -1 --oneline
docker compose -f deploy/docker-compose.yml \
  -f deploy/migration/docker-compose.target.yml config >/dev/null
```

发布前应确认工作区干净、提交已在个人 fork 的独立发布分支中，并确认待发布
提交不是误包含的 `.env`、数据库备份、OAuth 凭据或本地数据。

### 备份和保留回滚镜像

生产操作前先执行现有备份脚本并确认产物可读：

```bash
cd /opt/sub2api
/opt/sub2api/deploy/migration/backup-sub2api.sh
```

在构建新镜像前给当前生产镜像增加不可变回滚标签。标签名应记录时间或 Git
提交，例如：

```bash
docker tag weishaw/sub2api:local \
  weishaw/sub2api:rollback-$(date +%Y%m%d-%H%M%S)
```

### 构建并替换应用容器

生产 Compose 使用 `weishaw/sub2api:local`，因此在源码根目录构建同名本地
镜像，然后只重建应用服务：

```bash
cd /opt/sub2api
docker build --progress=plain -t weishaw/sub2api:local .

cd /opt/sub2api/deploy
docker compose \
  -f docker-compose.yml \
  -f migration/docker-compose.target.yml \
  up -d --no-deps --force-recreate sub2api
```

验证应用容器、健康检查和日志：

```bash
docker compose \
  -f docker-compose.yml \
  -f migration/docker-compose.target.yml ps
curl -fsS http://127.0.0.1:8080/health
docker logs --tail=100 sub2api
```

只要应用镜像能够启动并通过健康检查，就不应重建 PostgreSQL、Redis、Nginx
或 sing-box 容器。不要使用 `docker compose down -v`，也不要删除任何
`deploy_*` 生产卷。

### 回滚

如果新应用启动失败，使用发布前保存的回滚标签恢复镜像，然后只重建应用：

```bash
docker tag weishaw/sub2api:rollback-YYYYMMDD-HHMMSS weishaw/sub2api:local
cd /opt/sub2api/deploy
docker compose \
  -f docker-compose.yml \
  -f migration/docker-compose.target.yml \
  up -d --no-deps --force-recreate sub2api
```

数据库迁移通常是前向且不可逆的。应用镜像回滚不等于数据库回滚；若迁移
已经改变数据库结构，必须依据备份和对应迁移脚本制定恢复方案，不能盲目回滚
镜像后继续写入生产数据库。

## 4. Git 发布分支建议

建议保持个人 fork 的 `main` 跟随官方 `upstream/main`，把当前生产定制放在
独立分支，例如 `custom/production-adaptations`，日常修改再从该分支创建
`feature/*`。同步官方更新时先执行：

```bash
git fetch upstream main
git log --oneline --left-right main...upstream/main
```

当前本地定制与官方差异很大，不要一次性强制 rebase 或强推 fork 的 `main`；
应按功能拆分、逐项 cherry-pick，并在每个阶段重新运行测试和构建。
