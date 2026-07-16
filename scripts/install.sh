#!/bin/sh
#============================================================
#  WAN Manager 一键安装脚本
#  类似 ShellClash 风格的安装体验
#  项目地址: https://github.com/Wooo0/wan-manager
#
#  用法:
#    curl -sSL https://raw.githubusercontent.com/Wooo0/wan-manager/main/scripts/install.sh | sh
#    wget -qO- https://raw.githubusercontent.com/Wooo0/wan-manager/main/scripts/install.sh | sh
#
#  自定义安装目录:
#    curl -sSL ... | WM_INSTALL_DIR=/opt/wan-manager sh
#    curl -sSL ... | WM_INSTALL_DIR=/opt/wan-manager WM_CONFIG_DIR=/opt/wan-manager/etc sh
#============================================================

set -e

# 支持环境变量自定义安装路径
INSTALL_DIR="${WM_INSTALL_DIR:-/usr/bin}"
CONFIG_DIR="${WM_CONFIG_DIR:-/etc/wan-manager}"
INIT_DIR="${WM_INIT_DIR:-/etc/init.d}"
BINARY_NAME="wan-manager"
SERVICE_NAME="wan-manager"
SCRIPT_NAME="wan-manager.sh"

GITHUB_REPO="Wooo0/wan-manager"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}/main"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
GRAY='\033[1;30m'
BOLD='\033[1m'
NC='\033[0m'

print_banner() {
    echo ""
    echo -e "${CYAN}╔══════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║${NC}     ${WHITE}${BOLD}██╗    ██╗ █████╗ ███╗   ██╗    ${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}     ${WHITE}${BOLD}██║    ██║██╔══██╗████╗  ██║    ${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}     ${WHITE}${BOLD}██║ █╗ ██║███████║██╔██╗ ██║    ${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}     ${WHITE}${BOLD}██║███╗██║██╔══██║██║╚██╗██║    ${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}     ${WHITE}${BOLD}╚███╔███╔╝██║  ██║██║ ╚████║    ${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}      ${WHITE}${BOLD}╚══╝╚══╝ ╚═╝  ╚═╝╚═╝  ╚═══╝    ${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC}           ${WHITE}${BOLD}WAN Manager 智能分流管理系统${NC}               ${CYAN}║${NC}"
    echo -e "${CYAN}╚══════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_separator() {
    echo -e "${CYAN}──────────────────────────────────────────────────────${NC}"
}

get_latest_version() {
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -sL "$GITHUB_API" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "$GITHUB_API" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    fi
    if [ -z "$VERSION" ]; then
        echo ""
        return 1
    fi
    echo "$VERSION"
}

download_file() {
    url="$1"
    output="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -sSL -o "$output" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$output" "$url"
    else
        return 1
    fi
}

get_router_ip() {
    uci get network.lan.ipaddr 2>/dev/null || ip addr show br-lan 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1 | head -1 || echo "192.168.1.1"
}

#============================================================
#  交互式路径选择
#============================================================

select_install_path() {
    # 如果环境变量已设置，跳过交互
    if [ -n "$WM_INSTALL_DIR" ]; then
        return 0
    fi

    print_banner
    echo -e "  ${WHITE}${BOLD}◆ 选择安装路径${NC}"
    print_separator
    echo ""
    echo -e "    ${WHITE}1.${NC} 默认路径 ${GRAY}(推荐)${NC}"
    echo -e "       程序: /usr/bin/wan-manager"
    echo -e "       配置: /etc/wan-manager/"
    echo -e "       启动: /etc/init.d/S99wan-manager"
    echo ""
    echo -e "    ${WHITE}2.${NC} 自定义路径"
    echo -e "       自行指定程序、配置、启动脚本的安装位置"
    echo ""
    print_separator
    printf "  请选择 [1-2] (默认 1): "
    read -r path_choice

    case "$path_choice" in
        2)
            echo ""
            printf "  程序安装目录 ${GRAY}[/usr/bin]${NC}: "
            read -r custom_install
            if [ -n "$custom_install" ]; then
                INSTALL_DIR="$custom_install"
            fi

            printf "  配置文件目录 ${GRAY}[/etc/wan-manager]${NC}: "
            read -r custom_config
            if [ -n "$custom_config" ]; then
                CONFIG_DIR="$custom_config"
            fi

            printf "  启动脚本目录 ${GRAY}[/etc/init.d]${NC}: "
            read -r custom_init
            if [ -n "$custom_init" ]; then
                INIT_DIR="$custom_init"
            fi

            echo ""
            echo -e "  ${CYAN}安装路径确认:${NC}"
            echo -e "    程序: ${WHITE}${INSTALL_DIR}/${BINARY_NAME}${NC}"
            echo -e "    配置: ${WHITE}${CONFIG_DIR}/config.toml${NC}"
            echo -e "    启动: ${WHITE}${INIT_DIR}/S99wan-manager${NC}"
            echo ""
            printf "  确认? [Y/n]: "
            read -r confirm
            case "$confirm" in
                [nN]|[nN][oO])
                    echo -e "  ${YELLOW}已取消，重新选择${NC}"
                    sleep 1
                    select_install_path
                    ;;
            esac
            ;;
        *)
            # 默认路径，无需操作
            ;;
    esac
}

#============================================================
#  主安装流程
#============================================================

print_banner

echo -e "  ${CYAN}正在检查系统环境...${NC}"
echo ""

# 检查架构
ARCH=$(uname -m)
case "$ARCH" in
    aarch64|arm64)
        PLATFORM="linux-arm64"
        ARCH_LABEL="ARM64 (aarch64)"
        ;;
    armv7l|armhf)
        PLATFORM="linux-armv7"
        ARCH_LABEL="ARMv7 (armhf)"
        ;;
    x86_64|amd64)
        PLATFORM="linux-amd64"
        ARCH_LABEL="AMD64 (x86_64)"
        ;;
    *)
        echo -e "  ${RED}✗ 不支持的架构: $ARCH${NC}"
        echo -e "  ${YELLOW}目前支持: aarch64/arm64, armv7l/armhf, x86_64/amd64${NC}"
        exit 1
        ;;
esac
echo -e "  系统架构: ${WHITE}$ARCH_LABEL${NC}"

# 检查系统
KERNEL=$(uname -s)
echo -e "  操作系统: ${WHITE}$KERNEL $(uname -r)${NC}"

# 检查下载工具
if command -v curl >/dev/null 2>&1; then
    DOWNLOADER="curl"
elif command -v wget >/dev/null 2>&1; then
    DOWNLOADER="wget"
else
    echo -e "  ${RED}✗ 找不到 curl 或 wget，请先安装下载工具${NC}"
    exit 1
fi
echo -e "  下载工具: ${WHITE}$DOWNLOADER${NC}"

# 交互式选择安装路径
select_install_path

print_separator
echo ""
echo -e "  ${CYAN}正在获取最新版本...${NC}"

VERSION=$(get_latest_version)
if [ -z "$VERSION" ]; then
    echo -e "  ${RED}✗ 无法获取最新版本号${NC}"
    echo -e "  ${YELLOW}请检查网络连接或稍后重试${NC}"
    exit 1
fi

echo -e "  最新版本: ${GREEN}$VERSION${NC}"
echo -e "  目标平台: ${CYAN}$PLATFORM${NC}"

print_separator
echo ""
echo -e "  ${CYAN}正在下载 WAN Manager...${NC}"
echo ""

TMP_DIR=$(mktemp -d)
cd "$TMP_DIR"

TARBALL="wan-manager-${PLATFORM}.tar.gz"
DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${TARBALL}"

echo -e "  下载地址:"
echo -e "  ${WHITE}$DOWNLOAD_URL${NC}"
echo ""

if ! download_file "$DOWNLOAD_URL" "$TARBALL"; then
    echo -e "  ${RED}✗ 下载失败${NC}"
    cd /
    rm -rf "$TMP_DIR"
    exit 1
fi

echo -e "  ${GREEN}✓ 下载完成${NC}"
echo -e "  正在解压..."

if ! tar -xzf "$TARBALL" 2>/dev/null; then
    echo -e "  ${RED}✗ 解压失败${NC}"
    cd /
    rm -rf "$TMP_DIR"
    exit 1
fi

if [ ! -f "$BINARY_NAME" ]; then
    echo -e "  ${RED}✗ 二进制文件不存在，可能包格式错误${NC}"
    cd /
    rm -rf "$TMP_DIR"
    exit 1
fi

echo -e "  ${GREEN}✓ 解压完成${NC}"
echo ""
echo -e "  ${CYAN}正在安装...${NC}"
echo ""

# 安装二进制文件
chmod +x "$BINARY_NAME"
cp "$BINARY_NAME" "${INSTALL_DIR}/${BINARY_NAME}"
echo -e "  ${GREEN}✓${NC} 二进制文件: ${INSTALL_DIR}/${BINARY_NAME}"

# 创建配置目录
mkdir -p "$CONFIG_DIR"
echo -e "  ${GREEN}✓${NC} 配置目录: ${CONFIG_DIR}"

# 下载默认配置
if [ ! -f "${CONFIG_DIR}/config.toml" ]; then
    if download_file "${GITHUB_RAW}/deploy/config.toml.example" "${CONFIG_DIR}/config.toml"; then
        echo -e "  ${GREEN}✓${NC} 默认配置: ${CONFIG_DIR}/config.toml"
    else
        echo -e "  ${YELLOW}⚠${NC} 默认配置下载失败，请稍后手动创建"
    fi
else
    echo -e "  ${YELLOW}⚠${NC} 配置文件已存在，跳过 (${CONFIG_DIR}/config.toml)"
fi

# 下载启动脚本
if download_file "${GITHUB_RAW}/deploy/S99wan-manager" "${INIT_DIR}/S99wan-manager"; then
    chmod +x "${INIT_DIR}/S99wan-manager"
    # 替换启动脚本中的默认路径
    sed -i "s|/usr/bin/wan-manager|${INSTALL_DIR}/${BINARY_NAME}|g" "${INIT_DIR}/S99wan-manager" 2>/dev/null
    sed -i "s|/etc/wan-manager/config.toml|${CONFIG_DIR}/config.toml|g" "${INIT_DIR}/S99wan-manager" 2>/dev/null
    echo -e "  ${GREEN}✓${NC} 启动脚本: ${INIT_DIR}/S99wan-manager"

    # 设置开机自启
    if [ -d /etc/rc.d ]; then
        ln -sf "${INIT_DIR}/S99wan-manager" /etc/rc.d/S99wan-manager 2>/dev/null
        echo -e "  ${GREEN}✓${NC} 开机自启: 已启用"
    fi
else
    echo -e "  ${YELLOW}⚠${NC} 启动脚本下载失败，将使用 nohup 模式运行"
fi

# 下载管理脚本
if download_file "${GITHUB_RAW}/scripts/wan-manager.sh" "${INSTALL_DIR}/${SCRIPT_NAME}"; then
    chmod +x "${INSTALL_DIR}/${SCRIPT_NAME}"
    # 替换管理脚本中的默认路径
    sed -i "s|INSTALL_DIR=\"/usr/bin\"|INSTALL_DIR=\"${INSTALL_DIR}\"|g" "${INSTALL_DIR}/${SCRIPT_NAME}" 2>/dev/null
    sed -i "s|CONFIG_DIR=\"/etc/wan-manager\"|CONFIG_DIR=\"${CONFIG_DIR}\"|g" "${INSTALL_DIR}/${SCRIPT_NAME}" 2>/dev/null
    sed -i "s|INIT_SCRIPT=\"/etc/init.d/S99wan-manager\"|INIT_SCRIPT=\"${INIT_DIR}/S99wan-manager\"|g" "${INSTALL_DIR}/${SCRIPT_NAME}" 2>/dev/null
    echo -e "  ${GREEN}✓${NC} 管理脚本: ${INSTALL_DIR}/${SCRIPT_NAME}"
else
    echo -e "  ${YELLOW}⚠${NC} 管理脚本下载失败"
fi

cd /
rm -rf "$TMP_DIR"

ROUTER_IP=$(get_router_ip)

print_banner
echo -e "  ${GREEN}${BOLD}◆ 安装成功！${NC}"
print_separator
echo ""
echo -e "  版本: ${WHITE}$VERSION${NC}"
echo -e "  架构: ${WHITE}$ARCH_LABEL${NC}"
echo ""
echo -e "  安装路径:"
echo -e "    二进制文件: ${CYAN}${INSTALL_DIR}/${BINARY_NAME}${NC}"
echo -e "    管理脚本:   ${CYAN}${INSTALL_DIR}/${SCRIPT_NAME}${NC}"
echo -e "    配置文件:   ${CYAN}${CONFIG_DIR}/config.toml${NC}"
echo -e "    启动脚本:   ${CYAN}${INIT_DIR}/S99wan-manager${NC}"
echo ""
echo -e "  Web 管理地址:"
echo -e "    ${CYAN}http://${ROUTER_IP}:8899${NC}"
echo ""
echo -e "  API 接口:"
echo -e "    ${CYAN}http://${ROUTER_IP}:8899/api/v1/summary${NC}"
echo -e "    ${CYAN}http://${ROUTER_IP}:8899/api/v1/wan${NC}"
echo -e "    ${CYAN}http://${ROUTER_IP}:8899/api/v1/clients${NC}"
echo ""
echo -e "  命令行管理:"
echo -e "    ${WHITE}${SCRIPT_NAME}${NC}          进入交互管理面板"
echo -e "    ${WHITE}${SCRIPT_NAME} start${NC}      启动服务"
echo -e "    ${WHITE}${SCRIPT_NAME} stop${NC}       停止服务"
echo -e "    ${WHITE}${SCRIPT_NAME} restart${NC}    重启服务"
echo -e "    ${WHITE}${SCRIPT_NAME} status${NC}     查看状态"
echo -e "    ${WHITE}${SCRIPT_NAME} update${NC}     升级版本"
echo -e "    ${WHITE}${SCRIPT_NAME} log${NC}        查看日志"
echo -e "    ${WHITE}${SCRIPT_NAME} config${NC}     编辑配置"
echo ""
print_separator

# 询问是否立即启动并进入管理面板
echo ""
printf "  ${GREEN}是否立即启动服务并进入管理面板? [Y/n]: ${NC}"
read -r choice

case "$choice" in
    [nN]|[nN][oO])
        echo ""
        echo -e "  ${YELLOW}安装完成！你可以手动执行 ${WHITE}${SCRIPT_NAME}${YELLOW} 进入管理面板${NC}"
        echo ""
        ;;
    *)
        echo ""
        echo -e "  ${CYAN}正在启动服务...${NC}"

        # 启动服务
        if [ -x "${INIT_DIR}/S99wan-manager" ]; then
            "${INIT_DIR}/S99wan-manager" start >/dev/null 2>&1
        else
            nohup "${INSTALL_DIR}/${BINARY_NAME}" -config "${CONFIG_DIR}/config.toml" > /var/log/wan-manager.log 2>&1 &
            echo $! > /var/run/wan-manager.pid
        fi

        sleep 2

        # 检查是否启动成功
        if [ -f /var/run/wan-manager.pid ] && kill -0 "$(cat /var/run/wan-manager.pid)" 2>/dev/null; then
            echo -e "  ${GREEN}✓ 服务启动成功${NC}"
            sleep 1
        else
            echo -e "  ${YELLOW}⚠ 服务可能未正常启动，请进入面板后检查日志${NC}"
            sleep 2
        fi

        # 进入管理面板
        if [ -x "${INSTALL_DIR}/${SCRIPT_NAME}" ]; then
            exec "${INSTALL_DIR}/${SCRIPT_NAME}"
        fi
        ;;
esac
