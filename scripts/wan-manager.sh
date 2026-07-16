#!/bin/sh
#============================================================
#  WAN Manager 管理脚本
#  类似 ShellClash 风格的交互式管理面板
#  项目地址: https://github.com/Wooo0/wan-manager
#============================================================

INSTALL_DIR="/usr/bin"
CONFIG_DIR="/etc/wan-manager"
CONFIG_FILE="${CONFIG_DIR}/config.toml"
INIT_SCRIPT="/etc/init.d/S99wan-manager"
LOG_FILE="/var/log/wan-manager.log"
PID_FILE="/var/run/wan-manager.pid"
BINARY="${INSTALL_DIR}/wan-manager"
SCRIPT_NAME="wan-manager"

# 路径自检：如果脚本不在默认 INSTALL_DIR，说明是自定义安装，自动切换到脚本所在目录
# 避免 update 时重置路径导致写入 /usr/bin（小米路由器该路径只读）
_script_self="$(readlink -f "$0" 2>/dev/null || echo "$0")"
_script_dir="$(dirname "$_script_self")"
if [ "$_script_dir" != "/usr/bin" ] && [ -n "$_script_dir" ]; then
    # 脚本所在目录就是 INSTALL_DIR
    INSTALL_DIR="$_script_dir"
    BINARY="${INSTALL_DIR}/wan-manager"
    # 配置目录优先用同级 etc，不存在则回退默认
    if [ -d "${INSTALL_DIR}/etc" ]; then
        CONFIG_DIR="${INSTALL_DIR}/etc"
    fi
    CONFIG_FILE="${CONFIG_DIR}/config.toml"
    # 日志和 PID 文件也放安装目录，避免 /var/log 和 /var/run 在重启后清空
    LOG_FILE="${INSTALL_DIR}/wan-manager.log"
    PID_FILE="${INSTALL_DIR}/wan-manager.pid"
fi

GITHUB_REPO="Wooo0/wan-manager"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}/main"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
GRAY='\033[1;30m'
BG_BLUE='\033[44m'
BG_GREEN='\033[42m'
BG_RED='\033[41m'
BOLD='\033[1m'
NC='\033[0m'

clear_screen() {
    clear
}

print_banner() {
    clear_screen
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
}

print_separator() {
    echo -e "${CYAN}──────────────────────────────────────────────────────${NC}"
}

print_menu_title() {
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ $1${NC}"
    echo -e "  ${GRAY}──────────────────────────────────────────────────────${NC}"
}

pause() {
    echo ""
    printf "  ${YELLOW}按回车键继续...${NC}"
    read -r _
}

get_pid() {
    if [ -f "$PID_FILE" ]; then
        cat "$PID_FILE"
    else
        pgrep -f "wan-manager" | grep -v "wan-manager.sh" | head -1
    fi
}

is_running() {
    pid=$(get_pid)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        return 0
    fi
    return 1
}

get_current_version() {
    if [ -f "$BINARY" ]; then
        # 加超时保护，避免二进制卡住阻塞脚本
        ver=$(timeout 3 "$BINARY" --version 2>/dev/null | awk '{print $2}')
        if [ -n "$ver" ]; then
            echo "$ver"
        else
            echo "未知"
        fi
    else
        echo "未安装"
    fi
}

get_latest_version() {
    if command -v curl >/dev/null 2>&1; then
        curl -sL "$GITHUB_API" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/'
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$GITHUB_API" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/'
    fi
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

get_run_time() {
    pid=$(get_pid)
    if [ -n "$pid" ]; then
        if [ -f "/proc/$pid/stat" ]; then
            start_time=$(awk '{print $22}' /proc/$pid/stat 2>/dev/null)
            clk_tck=$(getconf CLK_TCK 2>/dev/null || echo 100)
            if [ -n "$start_time" ] && [ -n "$clk_tck" ]; then
                now=$(awk '{print $1}' /proc/uptime 2>/dev/null)
                if [ -n "$now" ]; then
                    seconds=$(echo "$now - $start_time / $clk_tck" | bc 2>/dev/null)
                    seconds=${seconds%.*}
                    if [ -n "$seconds" ] && [ "$seconds" -gt 0 ]; then
                        days=$((seconds / 86400))
                        hours=$(( (seconds % 86400) / 3600 ))
                        mins=$(( (seconds % 3600) / 60 ))
                        if [ "$days" -gt 0 ]; then
                            echo "${days}天${hours}小时${mins}分"
                        elif [ "$hours" -gt 0 ]; then
                            echo "${hours}小时${mins}分"
                        else
                            echo "${mins}分钟"
                        fi
                        return
                    fi
                fi
            fi
        fi
    fi
    echo "未知"
}

get_listen_port() {
    port=""
    if [ -f "$CONFIG_FILE" ]; then
        port=$(grep 'listen_addr' "$CONFIG_FILE" 2>/dev/null | awk -F'=' '{print $2}' | tr -d ' "' | awk -F':' '{print $2}')
    fi
    if [ -n "$port" ]; then
        echo "$port"
    else
        echo "8899"
    fi
}

get_router_ip() {
    uci get network.lan.ipaddr 2>/dev/null || ip addr show br-lan 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1 | head -1 || echo "127.0.0.1"
}

show_status_bar() {
    echo ""
    if is_running; then
        echo -e "  运行状态: ${GREEN}● 运行中${NC}    版本: ${WHITE}$(get_current_version)${NC}"
        echo -e "  运行时长: ${CYAN}$(get_run_time)${NC}    监听端口: ${CYAN}$(get_listen_port)${NC}"
    else
        echo -e "  运行状态: ${RED}● 已停止${NC}    版本: ${WHITE}$(get_current_version)${NC}"
    fi
    print_separator
}

#============================================================
#  服务管理
#============================================================

start_service() {
    print_banner
    echo ""
    echo -e "  ${GREEN}正在启动 WAN Manager 服务...${NC}"
    echo ""

    if [ ! -f "$BINARY" ]; then
        echo -e "  ${RED}✗ 找不到二进制文件: $BINARY${NC}"
        echo -e "  ${YELLOW}请先运行安装脚本${NC}"
        pause
        return 1
    fi

    if [ ! -f "$CONFIG_FILE" ]; then
        echo -e "  ${YELLOW}配置文件不存在，正在创建默认配置...${NC}"
        mkdir -p "$CONFIG_DIR"
        download_file "${GITHUB_RAW}/deploy/config.toml.example" "$CONFIG_FILE"
    fi

    if is_running; then
        echo -e "  ${YELLOW}服务已经在运行了 (PID: $(get_pid))${NC}"
        pause
        return 0
    fi

    if [ -x "$INIT_SCRIPT" ]; then
        "$INIT_SCRIPT" start >/dev/null 2>&1
    else
        nohup "$BINARY" -config "$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
        echo $! > "$PID_FILE"
    fi

    sleep 2

    if is_running; then
        echo -e "  ${GREEN}✓ 启动成功！${NC}"
        echo -e "  进程 PID: ${WHITE}$(get_pid)${NC}"
        echo -e "  Web 管理: ${CYAN}http://$(get_router_ip):$(get_listen_port)${NC}"
        echo -e "  API 地址: ${CYAN}http://$(get_router_ip):$(get_listen_port)/api/v1/summary${NC}"
    else
        echo -e "  ${RED}✗ 启动失败${NC}"
        echo -e "  ${YELLOW}请查看日志: $LOG_FILE${NC}"
        if [ -f "$LOG_FILE" ]; then
            echo ""
            echo -e "  ${GRAY}最近 10 行错误日志:${NC}"
            echo -e "  ${GRAY}──────────────────────────────────────────${NC}"
            tail -n 10 "$LOG_FILE" | sed 's/^/  /'
        fi
    fi
    pause
}

stop_service() {
    print_banner
    echo ""
    echo -e "  ${YELLOW}正在停止 WAN Manager 服务...${NC}"
    echo ""

    if ! is_running; then
        echo -e "  ${YELLOW}服务没有运行${NC}"
        pause
        return 0
    fi

    pid=$(get_pid)

    if [ -x "$INIT_SCRIPT" ]; then
        "$INIT_SCRIPT" stop >/dev/null 2>&1
    else
        kill "$pid" 2>/dev/null
        sleep 1
        if kill -0 "$pid" 2>/dev/null; then
            echo -e "  ${YELLOW}进程未响应，强制终止...${NC}"
            kill -9 "$pid" 2>/dev/null
        fi
        rm -f "$PID_FILE"
    fi

    echo -e "  ${GREEN}✓ 服务已停止${NC}"
    pause
}

restart_service() {
    print_banner
    echo ""
    echo -e "  ${YELLOW}正在重启 WAN Manager 服务...${NC}"
    echo ""

    if is_running; then
        pid=$(get_pid)
        if [ -x "$INIT_SCRIPT" ]; then
            "$INIT_SCRIPT" stop >/dev/null 2>&1
        else
            kill "$pid" 2>/dev/null
            sleep 1
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null
            fi
            rm -f "$PID_FILE"
        fi
        echo -e "  ${GRAY}已停止旧进程${NC}"
    fi

    if [ -x "$INIT_SCRIPT" ]; then
        "$INIT_SCRIPT" start >/dev/null 2>&1
    else
        nohup "$BINARY" -config "$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
        echo $! > "$PID_FILE"
    fi

    sleep 2

    if is_running; then
        echo -e "  ${GREEN}✓ 重启成功！${NC}"
        echo -e "  新进程 PID: ${WHITE}$(get_pid)${NC}"
    else
        echo -e "  ${RED}✗ 重启失败${NC}"
        echo -e "  ${YELLOW}请查看日志: $LOG_FILE${NC}"
    fi
    pause
}

#============================================================
#  状态与日志
#============================================================

show_status_detail() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 服务状态详情${NC}"
    print_separator

    if is_running; then
        echo -e "  运行状态: ${GREEN}● 运行中${NC}"
        echo -e "  进程 PID: ${WHITE}$(get_pid)${NC}"
        echo -e "  运行时长: ${CYAN}$(get_run_time)${NC}"
    else
        echo -e "  运行状态: ${RED}● 已停止${NC}"
    fi

    echo ""
    echo -e "  版本信息: ${WHITE}$(get_current_version)${NC}"
    echo -e "  二进制文件: $BINARY"
    echo -e "  配置文件: $CONFIG_FILE"
    echo -e "  日志文件: $LOG_FILE"
    echo -e "  PID 文件: $PID_FILE"

    port=$(get_listen_port)
    ip=$(get_router_ip)
    echo ""
    echo -e "  Web 管理地址: ${CYAN}http://${ip}:${port}${NC}"
    echo -e "  API 汇总接口: ${CYAN}http://${ip}:${port}/api/v1/summary${NC}"
    echo -e "  WAN 状态接口: ${CYAN}http://${ip}:${port}/api/v1/wan${NC}"
    echo -e "  客户端接口:   ${CYAN}http://${ip}:${port}/api/v1/clients${NC}"

    if is_running; then
        echo ""
        echo -e "  ${WHITE}${BOLD}◆ API 实时数据${NC}"
        print_separator
        url="http://127.0.0.1:${port}/api/v1/summary"
        if command -v curl >/dev/null 2>&1; then
            result=$(curl -s "$url" 2>/dev/null)
        elif command -v wget >/dev/null 2>&1; then
            result=$(wget -qO- "$url" 2>/dev/null)
        fi
        if [ -n "$result" ]; then
            echo "$result" | sed 's/^/  /'
        else
            echo -e "  ${YELLOW}API 暂无响应${NC}"
        fi
    fi

    if [ -f "$LOG_FILE" ]; then
        echo ""
        echo -e "  ${WHITE}${BOLD}◆ 最近 10 行日志${NC}"
        print_separator
        tail -n 10 "$LOG_FILE" | sed 's/^/  /'
    fi

    echo ""
    print_separator
    pause
}

show_logs() {
    lines=${1:-50}
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 日志查看 (最近 $lines 行)${NC}"
    print_separator
    echo ""

    if [ ! -f "$LOG_FILE" ]; then
        echo -e "  ${YELLOW}日志文件不存在${NC}"
        pause
        return
    fi

    tail -n "$lines" "$LOG_FILE" | sed 's/^/  /'

    echo ""
    print_separator
    echo -e "  ${WHITE}${BOLD}操作选项:${NC}"
    echo ""
    echo -e "    ${WHITE}1.${NC} 查看 50 行"
    echo -e "    ${WHITE}2.${NC} 查看 100 行"
    echo -e "    ${WHITE}3.${NC} 查看 200 行"
    echo -e "    ${WHITE}4.${NC} 查看全部日志"
    echo -e "    ${WHITE}5.${NC} 清空日志"
    echo -e "    ${WHITE}0.${NC} 返回主菜单"
    echo ""
    printf "  请选择: "
    read -r choice
    case "$choice" in
        1) show_logs 50 ;;
        2) show_logs 100 ;;
        3) show_logs 200 ;;
        4) show_logs 500 ;;
        5)
            echo ""
            printf "  ${RED}确认清空日志? [y/N]: ${NC}"
            read -r confirm
            case "$confirm" in
                [yY]|[yY][eE][sS])
                    echo "" > "$LOG_FILE"
                    echo -e "  ${GREEN}✓ 日志已清空${NC}"
                    sleep 1
                    ;;
            esac
            show_logs "$lines"
            ;;
        0) return ;;
        *) show_logs "$lines" ;;
    esac
}

#============================================================
#  配置管理
#============================================================

edit_config() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 配置文件管理${NC}"
    print_separator

    if [ ! -f "$CONFIG_FILE" ]; then
        echo ""
        echo -e "  ${YELLOW}配置文件不存在，正在创建默认配置...${NC}"
        mkdir -p "$CONFIG_DIR"
        download_file "${GITHUB_RAW}/deploy/config.toml.example" "$CONFIG_FILE"
    fi

    echo ""
    echo -e "  配置文件路径: ${CYAN}$CONFIG_FILE${NC}"
    echo ""

    echo -e "  ${WHITE}${BOLD}操作选项:${NC}"
    echo ""
    echo -e "    ${WHITE}1.${NC} 使用 vi 编辑"
    echo -e "    ${WHITE}2.${NC} 使用 vim 编辑"
    echo -e "    ${WHITE}3.${NC} 查看配置内容"
    echo -e "    ${WHITE}4.${NC} 恢复默认配置"
    echo -e "    ${WHITE}0.${NC} 返回主菜单"
    echo ""
    printf "  请选择: "
    read -r choice

    case "$choice" in
        1)
            if command -v vi >/dev/null 2>&1; then
                vi "$CONFIG_FILE"
            else
                echo -e "  ${RED}找不到 vi 编辑器${NC}"
                sleep 1
            fi
            edit_config
            ;;
        2)
            if command -v vim >/dev/null 2>&1; then
                vim "$CONFIG_FILE"
            else
                echo -e "  ${RED}找不到 vim 编辑器${NC}"
                sleep 1
            fi
            edit_config
            ;;
        3)
            print_banner
            echo ""
            echo -e "  ${WHITE}${BOLD}◆ 配置文件内容${NC}"
            print_separator
            echo ""
            cat "$CONFIG_FILE" | sed 's/^/  /'
            echo ""
            print_separator
            pause
            edit_config
            ;;
        4)
            echo ""
            printf "  ${YELLOW}确认恢复默认配置? 当前配置将被覆盖 [y/N]: ${NC}"
            read -r confirm
            case "$confirm" in
                [yY]|[yY][eE][sS])
                    download_file "${GITHUB_RAW}/deploy/config.toml.example" "$CONFIG_FILE"
                    echo -e "  ${GREEN}✓ 配置已恢复默认${NC}"
                    sleep 1
                    ;;
            esac
            edit_config
            ;;
        0) return ;;
        *) edit_config ;;
    esac
}

#============================================================
#  升级管理
#============================================================

update_service() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 版本升级${NC}"
    print_separator
    echo ""
    echo -e "  ${CYAN}正在检查最新版本...${NC}"

    latest=$(get_latest_version)
    if [ -z "$latest" ]; then
        echo -e "  ${RED}✗ 无法获取最新版本，请检查网络连接${NC}"
        echo -e "  ${YELLOW}GitHub API: $GITHUB_API${NC}"
        pause
        return 1
    fi

    current=$(get_current_version)

    echo ""
    echo -e "  当前版本: ${WHITE}$current${NC}"
    echo -e "  最新版本: ${GREEN}$latest${NC}"
    echo ""

    if [ "$current" = "$latest" ]; then
        echo -e "  ${GREEN}✓ 已经是最新版本，无需升级${NC}"
        pause
        return 0
    fi

    echo -e "  ${YELLOW}发现新版本，是否升级?${NC}"
    echo ""
    printf "  输入 ${GREEN}Y${NC} 确认升级，其他键取消: "
    read -r confirm
    case "$confirm" in
        [yY]|[yY][eE][sS])
            ;;
        *)
            echo -e "  ${YELLOW}已取消升级${NC}"
            pause
            return 0
            ;;
    esac

    ARCH=$(uname -m)
    case "$ARCH" in
        aarch64|arm64)
            PLATFORM="linux-arm64"
            ;;
        armv7l|armhf)
            PLATFORM="linux-armv7"
            ;;
        x86_64|amd64)
            PLATFORM="linux-amd64"
            ;;
        *)
            echo -e "  ${RED}✗ 不支持的架构: $ARCH${NC}"
            pause
            return 1
            ;;
    esac

    echo ""
    echo -e "  目标平台: ${CYAN}$PLATFORM${NC}"
    echo -e "  正在下载 wan-manager $latest ...${NC}"
    echo ""

    TMP_DIR=$(mktemp -d)
    TARBALL="wan-manager-${PLATFORM}.tar.gz"
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${latest}/${TARBALL}"

    echo -e "  下载地址: ${GRAY}$DOWNLOAD_URL${NC}"
    echo ""

    if ! download_file "$DOWNLOAD_URL" "${TMP_DIR}/${TARBALL}"; then
        echo -e "  ${RED}✗ 下载失败${NC}"
        rm -rf "$TMP_DIR"
        pause
        return 1
    fi

    echo -e "  ${GREEN}✓ 下载完成${NC}"
    echo -e "  正在解压..."

    cd "$TMP_DIR"
    tar -xzf "$TARBALL" 2>/dev/null

    if [ ! -f "wan-manager" ]; then
        echo -e "  ${RED}✗ 解压失败或文件格式错误${NC}"
        cd /
        rm -rf "$TMP_DIR"
        pause
        return 1
    fi

    echo -e "  ${GREEN}✓ 解压完成${NC}"

    was_running=false
    if is_running; then
        was_running=true
        echo -e "  正在停止当前服务..."
        pid=$(get_pid)
        if [ -x "$INIT_SCRIPT" ]; then
            "$INIT_SCRIPT" stop >/dev/null 2>&1
        else
            kill "$pid" 2>/dev/null
            sleep 1
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null
            fi
            rm -f "$PID_FILE"
        fi
    fi

    echo -e "  正在安装新版本..."
    chmod +x wan-manager
    cp wan-manager "$BINARY"

    echo -e "  正在更新管理脚本..."
    # 下载到临时文件，避免覆盖正在执行的自身脚本导致 shell 读取错位
    download_file "${GITHUB_RAW}/scripts/wan-manager.sh" "${INSTALL_DIR}/wan-manager.sh.new"
    if [ -f "${INSTALL_DIR}/wan-manager.sh.new" ]; then
        # 保持当前安装路径不变（避免新脚本重置为默认路径 /usr/bin）
        sed -i "s|INSTALL_DIR=\"/usr/bin\"|INSTALL_DIR=\"${INSTALL_DIR}\"|g" "${INSTALL_DIR}/wan-manager.sh.new" 2>/dev/null
        sed -i "s|CONFIG_DIR=\"/etc/wan-manager\"|CONFIG_DIR=\"${CONFIG_DIR}\"|g" "${INSTALL_DIR}/wan-manager.sh.new" 2>/dev/null
        sed -i "s|INIT_SCRIPT=\"/etc/init.d/S99wan-manager\"|INIT_SCRIPT=\"${INIT_SCRIPT}\"|g" "${INSTALL_DIR}/wan-manager.sh.new" 2>/dev/null
        chmod +x "${INSTALL_DIR}/wan-manager.sh.new"
        # mv 原子替换，不影响当前正在运行的脚本
        mv -f "${INSTALL_DIR}/wan-manager.sh.new" "${INSTALL_DIR}/wan-manager.sh"
    fi
    # 同步更新启动脚本路径（如果存在）
    if [ -f "$INIT_SCRIPT" ]; then
        sed -i "s|/usr/bin/wan-manager|${BINARY}|g" "$INIT_SCRIPT" 2>/dev/null
        sed -i "s|/etc/wan-manager/config.toml|${CONFIG_FILE}|g" "$INIT_SCRIPT" 2>/dev/null
        sed -i "s|/var/run/wan-manager.pid|${PID_FILE}|g" "$INIT_SCRIPT" 2>/dev/null
        sed -i "s|/var/log/wan-manager.log|${LOG_FILE}|g" "$INIT_SCRIPT" 2>/dev/null
    fi

    if [ ! -f "$CONFIG_FILE" ]; then
        mkdir -p "$CONFIG_DIR"
        download_file "${GITHUB_RAW}/deploy/config.toml.example" "$CONFIG_FILE"
    fi

    cd /
    rm -rf "$TMP_DIR"

    if [ "$was_running" = true ]; then
        echo -e "  正在重启服务..."
        if [ -x "$INIT_SCRIPT" ]; then
            "$INIT_SCRIPT" start >/dev/null 2>&1
        else
            nohup "$BINARY" -config "$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
            echo $! > "$PID_FILE"
        fi
        sleep 2
    fi

    echo ""
    echo -e "  ${GREEN}✓ 升级成功！${NC}"
    echo -e "  当前版本: ${WHITE}$(get_current_version)${NC}"

    if [ "$was_running" = true ] && is_running; then
        echo -e "  服务状态: ${GREEN}● 运行中${NC} (PID: $(get_pid))"
    fi

    echo ""
    echo -e "  ${GREEN}升级完成，按回车键退出...${NC}"
    read -r _
    # 注意：升级过程中已覆盖自身脚本，必须直接退出，不能继续执行原脚本后续内容
    exit 0
}

#============================================================
#  高级功能
#============================================================

advanced_menu() {
    while true; do
        print_banner
        echo ""
        echo -e "  ${WHITE}${BOLD}◆ 高级功能${NC}"
        print_separator
        echo ""
        echo -e "    ${WHITE}1.${NC} 测试 API 接口"
        echo -e "    ${WHITE}2.${NC} 查看 WAN 状态"
        echo -e "    ${WHITE}3.${NC} 查看客户端列表"
        echo -e "    ${WHITE}4.${NC} 查看系统资源"
        echo -e "    ${WHITE}5.${NC} 网络诊断"
        echo ""
        echo -e "    ${WHITE}0.${NC} 返回主菜单"
        echo ""
        show_status_bar
        printf "  请选择: "
        read -r choice

        case "$choice" in
            1) test_api ;;
            2) show_wan_status ;;
            3) show_clients ;;
            4) show_system_info ;;
            5) network_diagnose ;;
            0) return ;;
            *)
                echo ""
                echo -e "  ${RED}无效选项: $choice${NC}"
                sleep 1
                ;;
        esac
    done
}

test_api() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ API 接口测试${NC}"
    print_separator

    if ! is_running; then
        echo ""
        echo -e "  ${RED}✗ 服务未运行，请先启动服务${NC}"
        pause
        return
    fi

    port=$(get_listen_port)
    base_url="http://127.0.0.1:${port}"

    echo ""
    echo -e "  测试地址: ${CYAN}${base_url}${NC}"
    echo ""

    for endpoint in "/api/v1/health" "/api/v1/summary" "/api/v1/wan" "/api/v1/clients"; do
        url="${base_url}${endpoint}"
        echo -e "  ${WHITE}测试 ${endpoint}...${NC}"
        if command -v curl >/dev/null 2>&1; then
            result=$(curl -s "$url" 2>/dev/null)
            code=$(curl -s -o /dev/null -w "%{http_code}" "$url" 2>/dev/null)
        elif command -v wget >/dev/null 2>&1; then
            result=$(wget -qO- "$url" 2>/dev/null)
            code="200"
        fi
        if [ "$code" = "200" ] || [ -n "$result" ]; then
            echo -e "    ${GREEN}✓ 正常${NC}"
            echo -e "    ${GRAY}$(echo "$result" | head -c 100)...${NC}"
        else
            echo -e "    ${RED}✗ 失败${NC}"
        fi
        echo ""
    done

    print_separator
    pause
}

show_wan_status() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ WAN 口状态${NC}"
    print_separator

    if ! is_running; then
        echo ""
        echo -e "  ${RED}✗ 服务未运行${NC}"
        pause
        return
    fi

    port=$(get_listen_port)
    url="http://127.0.0.1:${port}/api/v1/wan"

    if command -v curl >/dev/null 2>&1; then
        result=$(curl -s "$url" 2>/dev/null)
    elif command -v wget >/dev/null 2>&1; then
        result=$(wget -qO- "$url" 2>/dev/null)
    fi

    echo ""
    if [ -n "$result" ]; then
        echo "$result" | sed 's/^/  /'
    else
        echo -e "  ${YELLOW}暂无数据${NC}"
    fi

    echo ""
    print_separator
    pause
}

show_clients() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 客户端设备列表${NC}"
    print_separator

    if ! is_running; then
        echo ""
        echo -e "  ${RED}✗ 服务未运行${NC}"
        pause
        return
    fi

    port=$(get_listen_port)
    url="http://127.0.0.1:${port}/api/v1/clients"

    if command -v curl >/dev/null 2>&1; then
        result=$(curl -s "$url" 2>/dev/null)
    elif command -v wget >/dev/null 2>&1; then
        result=$(wget -qO- "$url" 2>/dev/null)
    fi

    echo ""
    if [ -n "$result" ]; then
        echo "$result" | sed 's/^/  /'
    else
        echo -e "  ${YELLOW}暂无数据${NC}"
    fi

    echo ""
    print_separator
    pause
}

show_system_info() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 系统资源信息${NC}"
    print_separator
    echo ""

    echo -e "  系统: $(uname -a 2>/dev/null | sed 's/^/  /' | head -1)"
    echo ""

    if [ -f /proc/meminfo ]; then
        total_mem=$(grep MemTotal /proc/meminfo | awk '{print $2}')
        free_mem=$(grep MemFree /proc/meminfo | awk '{print $2}')
        cached_mem=$(grep Cached /proc/meminfo | awk '{print $2}')
        used_mem=$((total_mem - free_mem - cached_mem))
        total_mb=$((total_mem / 1024))
        used_mb=$((used_mem / 1024))
        free_mb=$((free_mem / 1024))
        echo -e "  内存:"
        echo -e "    总计: ${WHITE}${total_mb} MB${NC}"
        echo -e "    已用: ${YELLOW}${used_mb} MB${NC}"
        echo -e "    空闲: ${GREEN}${free_mb} MB${NC}"
    fi

    if [ -f /proc/loadavg ]; then
        load=$(cat /proc/loadavg)
        echo ""
        echo -e "  负载: ${WHITE}$load${NC}"
    fi

    if [ -f /proc/uptime ]; then
        uptime=$(awk '{print int($1)}' /proc/uptime)
        days=$((uptime / 86400))
        hours=$(( (uptime % 86400) / 3600 ))
        mins=$(( (uptime % 3600) / 60 ))
        echo -e "  运行时间: ${CYAN}${days}天${hours}小时${mins}分${NC}"
    fi

    echo ""
    print_separator
    pause
}

network_diagnose() {
    print_banner
    echo ""
    echo -e "  ${WHITE}${BOLD}◆ 网络诊断${NC}"
    print_separator
    echo ""

    echo -e "  ${CYAN}正在进行网络诊断...${NC}"
    echo ""

    echo -e "  ${WHITE}网络接口:${NC}"
    ip link show 2>/dev/null | grep -E '^[0-9]+:' | sed 's/^/    /'
    echo ""

    echo -e "  ${WHITE}WAN 接口流量:${NC}"
    for iface in pppoe-wan pppoe-wan_2 eth0 eth5; do
        if [ -d "/sys/class/net/$iface" ]; then
            rx=$(cat /sys/class/net/$iface/statistics/rx_bytes 2>/dev/null)
            tx=$(cat /sys/class/net/$iface/statistics/tx_bytes 2>/dev/null)
            rx_mb=$(echo "scale=2; $rx / 1024 / 1024" | bc 2>/dev/null)
            tx_mb=$(echo "scale=2; $tx / 1024 / 1024" | bc 2>/dev/null)
            echo -e "    ${CYAN}$iface${NC}: 下载 ${rx_mb} MB / 上传 ${tx_mb} MB"
        fi
    done
    echo ""

    echo -e "  ${WHITE}路由表:${NC}"
    ip route show | head -10 | sed 's/^/    /'
    echo ""

    echo -e "  ${WHITE}策略路由规则:${NC}"
    ip rule show | head -10 | sed 's/^/    /'

    echo ""
    print_separator
    pause
}

#============================================================
#  卸载
#============================================================

uninstall_service() {
    print_banner
    echo ""
    echo -e "  ${RED}${BOLD}◆ 卸载 WAN Manager${NC}"
    print_separator
    echo ""
    echo -e "  ${YELLOW}即将卸载以下内容:${NC}"
    echo -e "    - 二进制文件: $BINARY"
    echo -e "    - 管理脚本: ${INSTALL_DIR}/${SCRIPT_NAME}.sh"
    echo -e "    - 启动脚本: $INIT_SCRIPT"
    echo -e "    - 日志文件: $LOG_FILE"
    echo -e "    - PID 文件: $PID_FILE"
    echo ""
    echo -e "  ${GREEN}配置目录 $CONFIG_DIR 将被保留${NC}"
    echo ""

    printf "  ${RED}输入 YES 确认卸载: ${NC}"
    read -r confirm
    case "$confirm" in
        YES|yes)
            ;;
        *)
            echo -e "  ${YELLOW}已取消卸载${NC}"
            pause
            return
            ;;
    esac

    echo ""
    echo -e "  ${CYAN}正在卸载...${NC}"

    if is_running; then
        pid=$(get_pid)
        if [ -x "$INIT_SCRIPT" ]; then
            "$INIT_SCRIPT" stop >/dev/null 2>&1
        else
            kill "$pid" 2>/dev/null
            sleep 1
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null
            fi
            rm -f "$PID_FILE"
        fi
        echo -e "  ${GRAY}已停止服务${NC}"
    fi

    rm -f "$BINARY"
    rm -f "$INIT_SCRIPT"
    rm -f "$PID_FILE"
    rm -f "$LOG_FILE"
    rm -f /etc/rc.d/S99wan-manager 2>/dev/null
    rm -f "${INSTALL_DIR}/${SCRIPT_NAME}.sh" 2>/dev/null

    echo -e "  ${GREEN}✓ 卸载完成${NC}"
    echo ""
    echo -e "  配置文件仍保留在: ${CYAN}$CONFIG_DIR${NC}"
    echo -e "  如需彻底删除，请手动执行: rm -rf $CONFIG_DIR"
    echo ""

    printf "  按回车键退出..."
    read -r _
    clear_screen
    exit 0
}

#============================================================
#  主菜单
#============================================================

main_menu() {
    while true; do
        print_banner
        echo ""
        echo -e "  ${WHITE}${BOLD}◆ 主菜单${NC}"
        print_separator
        echo ""
        echo -e "    ${WHITE}1.${NC} 启动服务"
        echo -e "    ${WHITE}2.${NC} 停止服务"
        echo -e "    ${WHITE}3.${NC} 重启服务"
        echo -e "    ${WHITE}4.${NC} 查看状态"
        echo -e "    ${WHITE}5.${NC} 查看日志"
        echo -e "    ${WHITE}6.${NC} 配置管理"
        echo ""
        echo -e "    ${WHITE}7.${NC} 高级功能"
        echo -e "    ${WHITE}8.${NC} 升级版本"
        echo -e "    ${WHITE}9.${NC} 卸载程序"
        echo ""
        echo -e "    ${WHITE}0.${NC} 退出"
        echo ""
        show_status_bar
        printf "  请选择 [0-9]: "
        read -r choice

        case "$choice" in
            1) start_service ;;
            2) stop_service ;;
            3) restart_service ;;
            4) show_status_detail ;;
            5) show_logs 50 ;;
            6) edit_config ;;
            7) advanced_menu ;;
            8) update_service ;;
            9) uninstall_service ;;
            0|q|Q|quit|exit)
                clear_screen
                exit 0
                ;;
            *)
                echo ""
                echo -e "  ${RED}无效选项: $choice${NC}"
                sleep 1
                ;;
        esac
    done
}

#============================================================
#  入口
#============================================================

if [ $# -gt 0 ]; then
    case "$1" in
        start) start_service ;;
        stop) stop_service ;;
        restart) restart_service ;;
        status) show_status_detail ;;
        update|upgrade) update_service ;;
        log|logs) show_logs "${2:-50}" ;;
        config) edit_config ;;
        uninstall) uninstall_service ;;
        version) get_current_version; echo ;;
        advanced) advanced_menu ;;
        help|--help|-h)
            echo "WAN Manager 管理脚本"
            echo ""
            echo "用法: wan-manager.sh [命令]"
            echo ""
            echo "命令:"
            echo "  start       启动服务"
            echo "  stop        停止服务"
            echo "  restart     重启服务"
            echo "  status      查看状态"
            echo "  update      升级版本"
            echo "  log [行数]  查看日志（默认50行）"
            echo "  config      配置管理"
            echo "  advanced    高级功能"
            echo "  uninstall   卸载程序"
            echo "  version     查看版本"
            echo "  help        显示帮助"
            echo ""
            echo "无参数时进入交互菜单"
            ;;
        *)
            echo "未知命令: $1"
            echo "输入 wan-manager.sh help 查看帮助"
            exit 1
            ;;
    esac
else
    main_menu
fi
