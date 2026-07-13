#!/bin/bash
# =============================================================================
# Hunyuan Embedding API 手动测试脚本
# =============================================================================
# 使用方法:
#   export HY3_API_KEY="your-api-key"
#   bash scripts/test_hunyuan_embedding.sh
#
# API 文档:
#   POST http://hunyuanapi.woa.com/openapi/v1/embeddings
#   Auth: Bearer <api-key>
#   Model: hunyuan-embedding-20250716
#   限制: 单条最多 1024 tokens (超过自动截断)，批量最多 50 条
# =============================================================================

set -euo pipefail

# ---- 配置 ----
BASE_URL="${HUNYUAN_BASE_URL:-http://hunyuanapi.woa.com}"
ENDPOINT="${BASE_URL}/openapi/v1/embeddings"
API_KEY="${HY3_API_KEY:-}"
MODEL="hunyuan-embedding-20250716"

# ---- 颜色 ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# ---- 工具函数 ----
log_test() {
    echo ""
    echo -e "${CYAN}============================================================${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}============================================================${NC}"
}

log_ok() {
    echo -e "  ${GREEN}✅ PASS${NC}: $1"
}

log_warn() {
    echo -e "  ${YELLOW}⚠️  WARN${NC}: $1"
}

log_info() {
    echo -e "  ℹ️  $1"
}

# 发送请求并展示结果
call_api() {
    local desc="$1"
    local payload="$2"

    echo -e "  ${YELLOW}📤 Request:${NC}"
    echo "$payload" | sed 's/^/    /'

    echo ""
    echo -e "  ${YELLOW}📥 Response:${NC}"

    local response
    local http_code

    response=$(curl -s -w "\n%{http_code}" \
        -X POST "$ENDPOINT" \
        -H "Authorization: Bearer $API_KEY" \
        -H "Content-Type: application/json" \
        -d "$payload" 2>&1)

    http_code=$(echo "$response" | tail -1)
    local body
    body=$(echo "$response" | sed '$d')

    echo "    HTTP Status: $http_code"

    if [ "$http_code" = "200" ]; then
        # 尝试提取关键信息
        local dims
        dims=$(echo "$body" | python3 -c "
import json, sys
data = json.load(sys.stdin)
print(f'  count={len(data[\"data\"])}')
for i, d in enumerate(data['data']):
    emb = d['embedding']
    print(f'  embedding[{i}]: dims={len(emb)}, first_3={emb[:3]}, last_3={emb[-3:]}')
print(f'  model={data.get(\"model\",\"?\")}')
print(f'  usage={data.get(\"usage\",{})}')
" 2>&1 || echo "    (python3 parse failed, raw body follows)")

        echo -e "    ${GREEN}$dims${NC}"

        if echo "$dims" | grep -q "python3 parse failed"; then
            echo "$body" | python3 -m json.tool 2>/dev/null | sed 's/^/    /' || echo "$body" | sed 's/^/    /'
        fi
    else
        echo -e "    ${RED}Error:${NC}"
        echo "$body" | python3 -m json.tool 2>/dev/null | sed 's/^/    /' || echo "$body" | sed 's/^/    /'
    fi
}

# ---- 前置检查 ----
echo "============================================================"
echo "  Hunyuan Embedding API Test Suite"
echo "============================================================"
echo "  Endpoint: $ENDPOINT"
echo "  Model:    $MODEL"
echo ""

if [ -z "$API_KEY" ]; then
    echo -e "${RED}ERROR: HY3_API_KEY environment variable is not set.${NC}"
    echo "  Run: export HY3_API_KEY=\"your-api-key\""
    exit 1
fi
log_info "API Key: ${API_KEY:0:8}...${API_KEY: -4}"

# ---- Test 1: 基本单条 embedding ----
log_test "Test 1: 基本单条 embedding (短文本)"
call_api "basic-embed" '{
  "model": "hunyuan-embedding-20250716",
  "input": "你好，世界！这是一个测试。"
}'

# ---- Test 2: 英文长文本 (~500 tokens) ----
log_test "Test 2: 英文中等长度文本 (~500 tokens)"
LONG_ENGLISH="The quick brown fox jumps over the lazy dog. "  # ~10 tokens
# 重复到约 500 tokens
TEXT_500TOK=""
for i in $(seq 1 50); do
    TEXT_500TOK="${TEXT_500TOK}${LONG_ENGLISH}"
done
log_info "Input length: ${#TEXT_500TOK} chars (estimated ~500 tokens)"
payload=$(python3 -c '
import json, sys
print(json.dumps({"model": "hunyuan-embedding-20250716", "input": sys.argv[1]}))
' "$TEXT_500TOK")
call_api "medium-english" "$payload"

# ---- Test 3: 中文长文本 (测试 token 限制) ----
log_test "Test 3: 中文长文本 (测试 1024 token 限制)"
# 中文字符的 token 比例大约是 1.5-2 chars/token
# 生成约 3000 中文字符，可能超过 1024 tokens
CN_TEXT="人工智能技术正在深刻地改变着我们的生活方式和工作模式。从自然语言处理到计算机视觉，从自动驾驶到医疗诊断，AI的应用场景不断拓展。深度学习模型的参数量持续增长，大语言模型展现出了令人惊叹的能力。然而，我们也需要关注AI伦理、数据隐私和算法公平性等重要问题。"
# 重复多次以达到超量文本
LARGE_CN=""
for i in $(seq 1 20); do
    LARGE_CN="${LARGE_CN}${CN_TEXT}"
done
log_info "Input length: ${#LARGE_CN} chars, bytes: $(echo -n "$LARGE_CN" | wc -c)"
call_api "long-chinese" "$(python3 -c '
import json, sys
print(json.dumps({"model": "hunyuan-embedding-20250716", "input": sys.argv[1]}))
' "$LARGE_CN")"

# ---- Test 4: 批量 embedding ----
log_test "Test 4: 批量 embedding (4 条文本)"
call_api "batch-4" '{
  "model": "hunyuan-embedding-20250716",
  "input": [
    "苹果是一种水果",
    "Python是一门编程语言",
    "北京是中国的首都",
    "机器学习是人工智能的分支"
  ]
}'

# ---- Test 5: 单条最大限制测试 (接近 1024 tokens) ----
log_test "Test 5: 已知 1024 token 限制（后端自动截断）"
log_info "此测试仅验证超过限制时不会报错，且能正常返回 embedding"
log_info "(后端会自动截断超过 1024 tokens 的文本)"

# 生成一个肯定超过 1024 tokens 的文本
BIG_TEXT=""
for i in $(seq 1 100); do
    BIG_TEXT="${BIG_TEXT}这是第${i}个测试段落，用于验证Hunyuan Embedding API在输入文本超过1024个token限制时的自动截断行为。"
done
log_info "Input length: ${#BIG_TEXT} chars (远超 1024 tokens)"

call_api "over-limit" "$(python3 -c '
import json, sys
print(json.dumps({"model": "hunyuan-embedding-20250716", "input": sys.argv[1]}))
' "$BIG_TEXT")"

# ---- Test 6: 空文本 ----
log_test "Test 6: 空文本 (边界情况)"
call_api "empty-text" '{
  "model": "hunyuan-embedding-20250716",
  "input": ""
}'

# ---- 总结 ----
echo ""
echo "============================================================"
echo "  测试完成！"
echo ""
echo "  关键知识点："
echo "  - 限制是 1024 tokens（不是 bytes），超过自动截断"
echo "  - 批量请求最多 50 条 input"
echo "  - 响应为标准 OpenAI embeddings 格式"
echo "  - Embedding 维度：根据上面输出确认"
echo "  - 单条 input 为字符串，多条为字符串数组"
echo "============================================================"
