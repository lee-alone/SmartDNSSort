#!/bin/bash

# AdBlock 多规则源测试脚本

echo "=== AdBlock 多规则源测试 ==="
echo ""

# 测试 API 端点
API_BASE="http://localhost:8080/api"

echo "1. 检查 AdBlock 状态..."
curl -s "${API_BASE}/adblock/status" | jq '.'
echo ""

echo "2. 获取当前规则源列表..."
curl -s "${API_BASE}/adblock/sources" | jq '.'
echo ""

echo "3. 添加测试规则源..."
# 添加 EasyList
curl -s -X POST "${API_BASE}/adblock/sources" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://easylist.to/easylist/easylist.txt"}' | jq '.'

# 添加 EasyList China
curl -s -X POST "${API_BASE}/adblock/sources" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://easylist-downloads.adblockplus.org/easylistchina.txt"}' | jq '.'

echo ""

echo "4. 再次获取规则源列表（应该有2个源）..."
curl -s "${API_BASE}/adblock/sources" | jq '.'
echo ""

echo "5. 触发规则更新..."
curl -s -X POST "${API_BASE}/adblock/update" | jq '.'
echo ""

echo "6. 等待5秒让更新开始..."
sleep 5

echo "7. 检查更新后的状态..."
curl -s "${API_BASE}/adblock/status" | jq '.'
echo ""

echo "8. 测试域名拦截..."
# 测试已知的广告域名
curl -s -X POST "${API_BASE}/adblock/test" \
  -H "Content-Type: application/json" \
  -d '{"domain":"doubleclick.net"}' | jq '.'

curl -s -X POST "${API_BASE}/adblock/test" \
  -H "Content-Type: application/json" \
  -d '{"domain":"google.com"}' | jq '.'

echo ""
echo "=== 测试完成 ==="
