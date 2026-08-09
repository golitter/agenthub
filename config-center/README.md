# AgentHub Config Center

一个 example 驱动的本地配置编辑器：读取仓库中的模板文件，对照查看并填写对应的实际文件。

## 文件范围

| 环境 | example（只读） | actual（写入） | 格式 |
|---|---|---|---|
| Local | `backend/.env.example` | `backend/.env` | dotenv |
| Local | `backend/configs/config.example.yaml` | `backend/configs/config.yaml` | YAML |
| Local / Docker | `agentend/.env.example` | `agentend/.env` | dotenv |
| Local / Docker | `agentend/config.example.yaml` | `agentend/config.yaml` | YAML |
| Local / Docker | `agentend/agents.example.json` | `agentend/agents.json` | JSON |
| Docker | `docker/configs/backend/.env.example` | `docker/configs/backend/.env` | dotenv |
| Docker | `docker/configs/backend/config.example.yaml` | `docker/configs/backend/config.yaml` | YAML |

后端不维护 AgentHub 业务字段 Schema，也不把文件拆成业务字段表。页面直接展示两个完整文件。

支持 `.env.example → .env`、`.example.env → .env`、`config.example.yaml → config.yaml`、`agents.example.json → agents.json` 等命名方式。只扫描 profile 白名单目录，浏览器不能提交文件路径。

## 对照与写入规则

- 左侧展示 example 原始文本，使用只读编辑器
- 右侧展示 actual 原始文本，可直接编辑，包括密码等实际值
- actual 不存在时右侧为空，可点击“复制 example”后保存创建
- YAML/JSON 保存前校验语法；dotenv 保持用户输入的原始文本
- 保存写入右侧完整文件内容，不改写 example

页面分为选择环境、对照填写、审查保存、应用运行四步。保存前校验 revision、创建备份并原子替换；备份恢复位于页面底部。

最后一步需要用户确认后才运行固定命令：Local 执行 `make restart`；Docker 执行 `make docker-up` 后再执行 `make restart-agentend`。保存配置不会自动中断服务。

## 独立环境

```text
config-center/.venv/             # 独立 uv Python 环境
config-center/web/node_modules/  # 独立 Vite/React 环境
```

不复用 `agentend/.venv` 或 `frontend/node_modules`。系统需要安装 `uv` 和 `pnpm`；服务器也可以通过 `PNPM` 指定 pnpm 可执行文件。

## 运行

在仓库根目录执行：

```bash
make config-center
```

服务器没有全局 `pnpm` 时，从模板创建本机专用环境文件并填写 pnpm 绝对路径：

```bash
cp scripts/server-env.example.sh scripts/server-env.sh
# 编辑 scripts/server-env.sh
make config-center
```

`scripts/server-env.sh` 已被 Git 忽略，供各开发环境自行配置。Makefile 检测到它时，会在同一个 Bash recipe 中先 source，再启动 Config Center；页面第四步调用 `make restart` 时也采用相同方式。文件不存在时会直接继续，使用 PATH 中的 `pnpm`。

绕过 Make、直接管理三端服务时，才需要手动加载本地环境文件：

```bash
source scripts/server-env.sh
./scripts/run.sh restart
```

或者：

```bash
./config-center/run-config-center.sh
```

访问 `http://127.0.0.1:5174`。API 监听 `http://127.0.0.1:9100`。保持终端运行，按 `Ctrl+C` 同时停止 Web 和 API。

如果端口被旧版本占用，先在原终端按 `Ctrl+C`，再重新执行命令。

## 测试

```bash
make test-config-center
```

测试使用临时项目目录，不会写仓库中的真实配置。

## 安全边界

- Web/API 只监听 loopback
- 读取 actual 和写入 API 都需要启动期 session token，并校验 Origin
- 客户端只提交服务端发现的 file ID、revision 和完整 actual 文本，不能提交路径
- 应用运行 API 只能执行当前 profile 对应的固定 Makefile 命令，不接受客户端命令或参数
- actual 编辑器会显示文件中的密码等明文，因此只应在可信本机环境使用
- 写入前备份，事务失败时回滚
- 面向可信本机用户，不是远程或多租户配置服务
