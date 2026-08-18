# Config Center：example / actual 双栏文件编辑器

## 1. 交互模型

Config Center 直接对照两个完整文件，不把配置拆成业务字段表：

```text
┌──────── example（只读）────────┬──────── actual（可编辑）────────┐
│ config.example.yaml            │ config.yaml                     │
│ 完整原始文本                   │ 完整原始文本                    │
└───────────────────────────────┴─────────────────────────────────┘
```

顶部切换文件对。actual 不存在时右栏为空，用户可直接填写或复制 example。桌面使用左右双栏，窄屏改为上下排列。

Local / Docker 决定文件集合和最后一步的固定应用命令；配置保存本身不会自动重启服务。

## 2. 文件发现

服务端只扫描固定 profile 根目录第一层：

| Profile | 目录 |
|---|---|
| `local` | `backend/`、`backend/configs/`、`agentend/` |
| `docker` | `docker/configs/backend/`、`agentend/` |

支持 `.env.example → .env`、`.example.env → .env`、`name.example.yaml → name.yaml`、`name.example.yml → name.yml` 和 `name.example.json → name.json`。客户端使用服务端生成的 file ID，不能提交路径。

## 3. 读取与显示

配置响应包含 example 和 actual 的 UTF-8 原始文本。example textarea 为只读；actual textarea 可编辑，所以实际文件中的密码也可以查看和修改。读取 actual 必须携带启动期 session token，并通过 Origin 校验；该工具只面向可信本机用户。

后端不维护 AgentHub FieldSpec，不推断字段分组、类型或范围。example 修改后刷新页面即可看到完整新内容。

## 4. 校验与写入

浏览器提交 `profile + fileId + revision + complete actual content`。服务端重新发现 file ID 对应路径：

1. 校验 actual 当前 SHA-256 revision。
2. YAML/JSON 解析完整文本，拒绝语法错误；dotenv 保留原始文本。
3. 为已存在的 actual 创建备份。
4. 在 actual 同目录写临时文件并原子替换。
5. 校验最终内容；多文件失败时回滚。

example 文件永不写入。actual 的注释、顺序和额外字段完全由右侧文本决定，不经过结构化重排。备份恢复位于主编辑区下方的折叠区域。

## 5. 应用运行

用户保存后可进入第四步显式运行配置。Local 固定执行 `make restart`；Docker 固定执行 `make docker up`，成功后执行 `make restart-agentend`。API 不接受任意命令、target 或 shell 字符串，命令输出和退出码返回页面。运行和保存/恢复共用互斥锁。

`scripts/server-env.sh` 是被 Git 忽略的可选本地环境文件，由 `scripts/server-env.example.sh` 提供模板。相关 Make recipe 检测到实际文件时，会在同一个 Bash 中先 source，再执行 Config Center 或 `run.sh`；文件不存在时直接继续并使用 PATH 中的 `pnpm`。因此不同开发环境可以覆盖 `PNPM`，同时本地和服务器仍共用一套运行逻辑。

## 6. 文件范围

当前共有 7 个物理文件对，Local 与 Docker profile 各展示 5 个，格式覆盖 dotenv、YAML 和 JSON。Config Center 使用独立的 `config-center/.venv` 与 `config-center/web/node_modules`。

## 7. 验收标准

- 左侧完整 example 只读，右侧完整 actual 可编辑
- 密码等 actual 原始值在右侧可查看和修改
- actual 缺失时可以复制 example 并创建
- YAML/JSON 语法错误不能保存
- example 不会被任何保存请求修改
- 未知 file ID、过期 revision、路径逃逸被拒绝
- 保存前备份，失败回滚，备份可恢复
- 第四步需要确认后执行 profile 对应的固定运行命令，并展示输出
- Python、Web 测试和 Vite production build 全部通过
