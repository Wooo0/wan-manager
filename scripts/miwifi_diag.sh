#!/bin/sh
# ============================================
# 小米路由器 MiWiFi API 诊断脚本 v2
# 用法: sh rc01_diag.sh <admin密码>
# ============================================

ROUTER_IP="${ROUTER_IP:-127.0.0.1}"
API_BASE="http://${ROUTER_IP}/cgi-bin/luci"

echo "=========================================="
echo " 小米路由器 MiWiFi API 诊断 v2"
echo " 目标: $ROUTER_IP"
echo "=========================================="

# ====== 第1步 ======
echo ""
echo "[1/4] 检查 HTTP 服务..."
if curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 "${API_BASE}/api/xqsystem/init_info" | grep -q "200"; then
    echo "  ✅ OK"
else
    echo "  ❌ 不可达"
    exit 1
fi

# ====== 第2步: init_info ======
echo ""
echo "[2/4] 路由器基本信息..."
INIT_INFO=$(curl -s --connect-timeout 3 "${API_BASE}/api/xqsystem/init_info" 2>/dev/null)
HW_VERSION=$(echo "$INIT_INFO" | grep -o '"hardware":"[^"]*"' | cut -d'"' -f4)
ROM_VER=$(echo "$INIT_INFO" | grep -o '"romversion":"[^"]*"' | cut -d'"' -f4)
IS_REDMI=$(echo "$INIT_INFO" | grep -o '"isRedmi":[0-9]*' | grep -o '[0-9]*')
echo "  型号: $HW_VERSION, ROM: $ROM_VER"
echo "  类型: $( [ "$IS_REDMI" = "1" ] && echo 'Redmi' || echo 'Xiaomi')"

# ====== 第3步: 登录 ======
echo ""
echo "[3/4] 登录获取 stok..."

ADMIN_PASS="${1}"
if [ -z "$ADMIN_PASS" ]; then
    echo "  ❌ 请提供密码: sh $0 <admin密码>"
    exit 1
fi

# 生成 MAC - 格式: 小写+冒号 (Python: uuid.getnode() → ":".join(...))
MAC=$(cat /sys/class/net/br-lan/address 2>/dev/null | sed 's/../&:/g' | sed 's/:$//')
[ -z "$MAC" ] && MAC="dc:ad:be:ef:00:01"

TIMESTAMP=$(date +%s)
RANDOM_NUM=$(awk 'BEGIN{srand(); print int(rand()*1000)}')
NONCE="0_${MAC}_${TIMESTAMP}_${RANDOM_NUM}"

# SHA256(nonce + SHA256(password + public_key))
PUBLIC_KEY="a2ffa5c9be07488bbb04a3a47d3c5f6a"
HASH1=$(echo -n "${ADMIN_PASS}${PUBLIC_KEY}" | openssl dgst -sha256 | awk '{print $2}')
PWD_HASH=$(echo -n "${NONCE}${HASH1}" | openssl dgst -sha256 | awk '{print $2}')

echo "  nonce: ${NONCE}"
echo "  尝试 SHA256..."

LOGIN_RESP=$(curl -s --connect-timeout 5 \
    -d "username=admin&logtype=2&password=${PWD_HASH}&nonce=${NONCE}" \
    "${API_BASE}/api/xqsystem/login" 2>/dev/null)

TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
    echo "  ❌ SHA256 失败: ${LOGIN_RESP:0:200}"
    echo "  尝试 SHA1..."
    HASH1=$(echo -n "${ADMIN_PASS}${PUBLIC_KEY}" | openssl dgst -sha1 | awk '{print $2}')
    PWD_HASH=$(echo -n "${NONCE}${HASH1}" | openssl dgst -sha1 | awk '{print $2}')
    LOGIN_RESP=$(curl -s --connect-timeout 5 \
        -d "username=admin&logtype=2&password=${PWD_HASH}&nonce=${NONCE}" \
        "${API_BASE}/api/xqsystem/login" 2>/dev/null)
    TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
fi

if [ -z "$TOKEN" ]; then
    echo "  ❌ 登录失败: ${LOGIN_RESP:0:300}"
    exit 1
fi

echo "  ✅ 登录成功: ${TOKEN:0:12}..."

# ====== 第4步: 设备数据 ======
echo ""
echo "[4/4] 获取设备列表..."

# --- devicelist ---
DEVICE_LIST=$(curl -s --connect-timeout 5 "${API_BASE}/;stok=${TOKEN}/api/misystem/devicelist" 2>/dev/null)
DL_CODE=$(echo "$DEVICE_LIST" | grep -o '"code":[0-9]*' | head -1 | grep -o '[0-9]*')

if [ "$DL_CODE" = "0" ]; then
    echo "  ✅ misystem/devicelist"
    echo "$DEVICE_LIST" | python3 -c "
import json,sys
d=json.load(sys.stdin)
router_mac=d.get('mac','?')
devs=d.get('list',[])
print(f'  设备总数: {len(devs)} 台')
print(f'  路由器MAC: {router_mac}')
# 分类统计
wired=sum(1 for x in devs if x.get('type')==0)
wifi24=sum(1 for x in devs if x.get('type')==1)
wifi5=sum(1 for x in devs if x.get('type')==2)
guest=sum(1 for x in devs if x.get('type')==3)
isaps=sum(1 for x in devs if x.get('isap')==1)
print(f'  有线: {wired} | 2.4G: {wifi24} | 5G: {wifi5} | 访客: {guest} | Mesh节点: {isaps}')
print()
print('  --- 前 5 条样本 ---')
for i,dev in enumerate(devs[:5]):
    parent=dev.get('parent','')
    ip_info=dev.get('ip',[])
    ip=ip_info[0].get('ip','') if ip_info else ''
    print(f'  [{i+1}] {dev.get(\"name\",\"?\")} | mac={dev.get(\"mac\",\"?\")} | ip={ip} | type={dev.get(\"type\",-1)} | isap={dev.get(\"isap\",0)} | parent={parent[:17]}')
if len(devs)>5:
    print(f'  ... 共 {len(devs)} 台')
" 2>/dev/null || echo "  (python3 不可用，原始JSON前500字符): ${DEVICE_LIST:0:500}"
else
    echo "  ❌ devicelist: code=$DL_CODE"
fi

# --- wifi_connect_devices ---
echo ""
WIFI_DEVICES=$(curl -s --connect-timeout 5 "${API_BASE}/;stok=${TOKEN}/api/xqnetwork/wifi_connect_devices" 2>/dev/null)
WIFI_CODE=$(echo "$WIFI_DEVICES" | grep -o '"code":[0-9]*' | head -1 | grep -o '[0-9]*')
if [ "$WIFI_CODE" = "0" ]; then
    echo "  ✅ xqnetwork/wifi_connect_devices"
    echo "$WIFI_DEVICES" | python3 -c "
import json,sys
d=json.load(sys.stdin)
devs=d.get('list',[])
print(f'  WiFi设备: {len(devs)} 台')
for i,dev in enumerate(devs[:3]):
    sig=dev.get('signal','?')
    print(f'  [{i+1}] mac={dev.get(\"mac\",\"?\")} | signal={sig} dBm')
" 2>/dev/null || echo "  raw: ${WIFI_DEVICES:0:300}"
else
    echo "  ⚠️  wifi_connect_devices: code=$WIFI_CODE"
fi

# --- newstatus ---
echo ""
NEWSTATUS=$(curl -s --connect-timeout 5 "${API_BASE}/;stok=${TOKEN}/api/misystem/newstatus" 2>/dev/null)
NS_CODE=$(echo "$NEWSTATUS" | grep -o '"code":[0-9]*' | head -1 | grep -o '[0-9]*')
if [ "$NS_CODE" = "0" ]; then
    echo "  ✅ misystem/newstatus"
    echo "$NEWSTATUS" | python3 -c "
import json,sys
d=json.load(sys.stdin)
devs=d.get('dev',[])
print(f'  设备: {len(devs)} 台')
if devs:
    for dev in devs[:3]:
        print(f'  mac={dev.get(\"mac\",\"?\")} | up={dev.get(\"upspeed\",0)}bps | down={dev.get(\"downspeed\",0)}bps')
" 2>/dev/null || echo "  raw: ${NEWSTATUS:0:300}"
else
    echo "  ⚠️  newstatus: code=$NS_CODE (正常，非必需)"
fi

echo ""
echo "=========================================="
echo " 诊断完成"
echo "=========================================="
