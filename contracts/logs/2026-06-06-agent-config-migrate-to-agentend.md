# 2026-06-06-agent-config-migrate-to-agentend

## 变更原因

后端 `admin_service.go` 的 `GetAgents()` 硬编码了各 Agent CLI 的配置文件路径（如 `~/.opencode/config.json`），与实际路径不一致导致前端管理面板显示 "配置文件不存在或无法读取"。将配置文件读取职责从后端迁移至 agentend，由 agentend 根据用户在 `config.yaml` 中显式配置的路径读取文件内容，后端通过 API 获取结果。

## 变更文件

本次不涉及 `contracts/schemas/` 契约修改。`AgentInfo` 是后端内部 DTO，不在契约层定义。

## 对比结果

### Backend DTO 变更（非契约）

**`backend/internal/service/service.go` — AgentInfo**
```diff
 type AgentInfo struct {
     Type          string `json:"type"`
     Name          string `json:"name"`
     Description   string `json:"description"`
-    ConfigDir     string `json:"configDir"`
-    ConfigFile    string `json:"configFile"`
+    ConfigPath    string `json:"configPath"`
     ConfigContent string `json:"configContent,omitempty"`
 }
```

### Frontend Interface 变更（非契约）

**`frontend/src/lib/api.ts` — AgentInfo**
```diff
 export interface AgentInfo {
   type: string
   name: string
   description: string
-  configDir: string
-  configFile: string
+  configPath: string
   configContent: string
 }
```

## 跨端影响

- **AgentEnd**：新增 `GET /v1/agents/configs` API 端点 + `config.yaml` 新增 `agents` 配置段 + `agent_config.py` 新增 `get_agent_config_path()` 函数
- **Backend**：`admin_service.GetAgents()` 改为调 agentend API；`agentend_client` 新增 `GetAgentConfigs()` 方法；移除 `sanitizeConfig` 脱敏逻辑
- **Frontend**：`AgentInfo` interface 字段同步更新；`AgentOverviewPage` 显示字段从 `configDir` 改为 `configPath`
