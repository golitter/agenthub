#!/usr/bin/env bash
#
# 查询 GLM Coding Plan 账户的「额度上限（quota limit）」。
#
# 对应 ~/.claude/plugins/cache/zai-coding-plugins/glm-plan-usage/0.0.1/
#      skills/usage-query-skill/scripts/query-usage.mjs 中的 quota limit 部分。
#
# 依赖：curl；可选 jq（缺失时原样打印响应体）。
# 配置：从脚本同目录的 .env 加载（存在则自动 source；不存在则回退到已 export 的环境变量）：
#   ANTHROPIC_BASE_URL   - 平台入口，如 https://open.bigmodel.cn/api/anthropic
#   ANTHROPIC_AUTH_TOKEN - 认证凭证（原值直接作为 Authorization 头，无 Bearer 前缀）
#
set -euo pipefail

# 0. 加载脚本同目录下的 .env（若存在）；不存在则回退到已 export 的环境变量
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck source=/dev/null
  . "$ENV_FILE"
  set +a
fi

# 1. 校验环境变量（与 query-usage.mjs 一致，缺任一直接报错退出）
: "${ANTHROPIC_BASE_URL:?Error: ANTHROPIC_BASE_URL is not set}"
: "${ANTHROPIC_AUTH_TOKEN:?Error: ANTHROPIC_AUTH_TOKEN is not set}"

# 2. 提取 scheme://host，丢弃路径（等价于 mjs 的 new URL(baseUrl).origin）
base_domain=$(printf '%s' "$ANTHROPIC_BASE_URL" | sed -E 's|^(https?://[^/]+).*|\1|')

# 3. 判定平台（对应 mjs 里的 baseUrl.includes 判断）
case "$base_domain" in
  *api.z.ai)                              platform="ZAI"   ;;
  *open.bigmodel.cn|*dev.bigmodel.cn)     platform="ZHIPU" ;;
  *)
    echo "Error: Unrecognized ANTHROPIC_BASE_URL: $ANTHROPIC_BASE_URL" >&2
    exit 1 ;;
esac

# 4. quota 接口（注意：这条接口原脚本里 appendQueryParams=false，不带时间参数）
quota_url="${base_domain}/api/monitor/usage/quota/limit"

echo "Platform: $platform"
echo

# 5. 发 GET 请求；Authorization 头直接用 token 原值（无 Bearer 前缀，与 mjs 一致）
resp=$(curl -fsS -X GET "$quota_url" \
  -H "Authorization: ${ANTHROPIC_AUTH_TOKEN}" \
  -H "Accept-Language: en-US,en" \
  -H "Content-Type: application/json")

# 6. 字段美化（对应 mjs 的 processQuotaLimit）
if command -v jq >/dev/null 2>&1; then
  printf '%s\n' "$resp" | jq '
    (.data // .)
    | if .limits then
        .limits |= map(
            if .type == "TOKENS_LIMIT" then
              { type: "Token usage(5 Hour)", percentage: .percentage }
            elif .type == "TIME_LIMIT" then
              { type: "MCP usage(1 Month)",
                percentage:    .percentage,
                currentUsage:  .currentValue,
                totol:         .usage,          # 注：原 mjs 这里拼写为 totol
                usageDetails:  .usageDetails }
            else . end
          )
      else . end
  '
else
  # 没有 jq：原样打印响应体
  printf '%s\n' "$resp"
fi
