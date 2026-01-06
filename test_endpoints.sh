#!/bin/bash
# Test script for Antigravity Brain Server endpoints
# Usage: ./test_endpoints.sh [port]

PORT="${1:-8080}"
BASE="http://localhost:$PORT"
GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}=== Antigravity Brain Endpoint Tests ===${NC}"
echo "Target: $BASE"
echo ""

# Test 1: Status endpoint
echo -e "${CYAN}[1] Testing /status${NC}"
RESP=$(curl -s "$BASE/status")
if echo "$RESP" | grep -q "mode"; then
    echo -e "${GREEN}PASS${NC}: Status endpoint working"
    echo "  Mode: $(echo $RESP | jq -r '.mode')"
    echo "  Cache: $(echo $RESP | jq -r '.cache_id' | cut -c1-30)..."
else
    echo -e "${RED}FAIL${NC}: Status endpoint not responding"
fi
echo ""

# Test 2: Models endpoint
echo -e "${CYAN}[2] Testing /models${NC}"
RESP=$(curl -s "$BASE/models")
COUNT=$(echo "$RESP" | jq '.models | length')
if [ "$COUNT" -gt 0 ]; then
    echo -e "${GREEN}PASS${NC}: Models endpoint working ($COUNT models)"
    echo "  First: $(echo $RESP | jq -r '.models[0].name')"
else
    echo -e "${RED}FAIL${NC}: No models returned"
fi
echo ""

# Test 3: Files endpoint
echo -e "${CYAN}[3] Testing /files${NC}"
RESP=$(curl -s "$BASE/files")
COUNT=$(echo "$RESP" | jq '.files | length')
if [ "$COUNT" -gt 0 ]; then
    echo -e "${GREEN}PASS${NC}: Files endpoint working ($COUNT items)"
else
    echo -e "${RED}FAIL${NC}: No files returned"
fi
echo ""

# Test 4: OpenAI Models endpoint
echo -e "${CYAN}[4] Testing /v1/models (OpenAI compat)${NC}"
RESP=$(curl -s "$BASE/v1/models")
if echo "$RESP" | grep -q "data"; then
    COUNT=$(echo "$RESP" | jq '.data | length')
    echo -e "${GREEN}PASS${NC}: OpenAI models endpoint working ($COUNT models)"
else
    echo -e "${RED}FAIL${NC}: OpenAI models endpoint not responding"
fi
echo ""

# Test 5: Native chat endpoint
echo -e "${CYAN}[5] Testing /chat (native)${NC}"
RESP=$(curl -s -X POST "$BASE/chat" \
    -H "Content-Type: application/json" \
    -d '{"message":"Say hello in 5 words or less","model":"gemini-2.0-flash"}')
if echo "$RESP" | grep -q "text"; then
    echo -e "${GREEN}PASS${NC}: Native chat working"
    echo "  Response: $(echo $RESP | jq -r '.text' | head -c 50)..."
    echo "  Tokens: $(echo $RESP | jq -r '.prompt_tokens') in / $(echo $RESP | jq -r '.response_tokens') out"
else
    echo -e "${RED}FAIL${NC}: Native chat error"
    echo "  Error: $RESP"
fi
echo ""

# Test 6: OpenAI chat endpoint
echo -e "${CYAN}[6] Testing /v1/chat/completions (OpenAI compat)${NC}"
RESP=$(curl -s -X POST "$BASE/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d '{"model":"gpt-4","messages":[{"role":"user","content":"Say hi"}]}')
if echo "$RESP" | grep -q "choices"; then
    echo -e "${GREEN}PASS${NC}: OpenAI chat working"
    echo "  Response: $(echo $RESP | jq -r '.choices[0].message.content' | head -c 50)..."
else
    echo -e "${RED}FAIL${NC}: OpenAI chat error"
    echo "  Error: $RESP"
fi
echo ""

# Test 7: Tree endpoint
echo -e "${CYAN}[7] Testing /tree${NC}"
RESP=$(curl -s "$BASE/tree")
if echo "$RESP" | grep -q "tree"; then
    LINES=$(echo "$RESP" | jq -r '.tree' | wc -l)
    echo -e "${GREEN}PASS${NC}: Tree endpoint working ($LINES lines)"
else
    echo -e "${RED}FAIL${NC}: Tree endpoint not responding"
fi
echo ""

# Test 8: Subdirectory files
echo -e "${CYAN}[8] Testing /files?path=web${NC}"
RESP=$(curl -s "$BASE/files?path=web")
if echo "$RESP" | grep -q "files"; then
    COUNT=$(echo "$RESP" | jq '.files | length')
    echo -e "${GREEN}PASS${NC}: Subdirectory listing working ($COUNT items)"
else
    echo -e "${RED}FAIL${NC}: Subdirectory listing error"
fi
echo ""

echo -e "${CYAN}=== Tests Complete ===${NC}"

