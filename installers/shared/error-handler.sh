#!/bin/bash
#
# Sentinel Installer - Shared Error Handler Functions
# Cross-platform shell functions for Linux and macOS installers
#
# Usage:
#   source /path/to/error-handler.sh
#   init_logging "/var/log/sentinel"
#   log_info "Starting installation..."
#
# Reference ID Format: INS-XXXXXX-YYYYMMDD

# ============================================================================
# CONSTANTS AND DEFAULTS
# ============================================================================

# Error code categories
readonly ERR_INSTALL_BASE=100
readonly ERR_SERVICE_BASE=200
readonly ERR_CONFIG_BASE=300
readonly ERR_NETWORK_BASE=400
readonly ERR_UPGRADE_BASE=500
readonly ERR_UNINSTALL_BASE=600

# Specific error codes (matching Go constants)
readonly E_INSTALL_GENERAL_FAILURE=100
readonly E_EXTRACT_FAILED=101
readonly E_DISK_SPACE_INSUFFICIENT=102
readonly E_PERMISSION_DENIED=103
readonly E_PATH_NOT_FOUND=104
readonly E_PATH_NOT_WRITABLE=105
readonly E_FILE_IN_USE=106
readonly E_BINARY_CORRUPT=107
readonly E_CHECKSUM_MISMATCH=108
readonly E_PLATFORM_MISMATCH=109
readonly E_PREREQUISITES_MISSING=110
readonly E_TEMP_DIR_CREATION_FAILED=111
readonly E_DOWNLOAD_FAILED=112
readonly E_TIMEOUT=113

readonly E_SERVICE_GENERAL_FAILURE=200
readonly E_SERVICE_CREATE_FAILED=201
readonly E_SERVICE_START_FAILED=202
readonly E_SERVICE_STOP_FAILED=203
readonly E_SERVICE_ALREADY_EXISTS=204
readonly E_SERVICE_NOT_FOUND=205
readonly E_SERVICE_TIMEOUT=206

readonly E_CONFIG_GENERAL_FAILURE=300
readonly E_CONFIG_INVALID_JSON=301
readonly E_CONFIG_MISSING_SERVER=302
readonly E_CONFIG_MISSING_TOKEN=303
readonly E_CONFIG_WRITE_FAILED=304

readonly E_NETWORK_GENERAL_FAILURE=400
readonly E_SERVER_UNREACHABLE=401
readonly E_TOKEN_INVALID=402
readonly E_TOKEN_EXPIRED=403
readonly E_TOKEN_MAX_USES=404
readonly E_SSL_CERTIFICATE_ERROR=405

# Colors for terminal output
if [[ -t 1 ]]; then
    readonly COLOR_RED='\033[0;31m'
    readonly COLOR_GREEN='\033[0;32m'
    readonly COLOR_YELLOW='\033[1;33m'
    readonly COLOR_BLUE='\033[0;34m'
    readonly COLOR_CYAN='\033[0;36m'
    readonly COLOR_GRAY='\033[0;90m'
    readonly COLOR_NC='\033[0m' # No Color
else
    readonly COLOR_RED=''
    readonly COLOR_GREEN=''
    readonly COLOR_YELLOW=''
    readonly COLOR_BLUE=''
    readonly COLOR_CYAN=''
    readonly COLOR_GRAY=''
    readonly COLOR_NC=''
fi

# Global variables
SENTINEL_REF_ID=""
SENTINEL_LOG_FILE=""
SENTINEL_LOG_LEVEL="INFO"  # DEBUG, INFO, WARN, ERROR
SENTINEL_SILENT_MODE=false
SENTINEL_PLATFORM=""
SENTINEL_INSTALL_START_TIME=""

# ============================================================================
# PLATFORM DETECTION
# ============================================================================

# Detect the current platform
detect_platform() {
    local os_name
    os_name=$(uname -s | tr '[:upper:]' '[:lower:]')

    case "$os_name" in
        linux)
            SENTINEL_PLATFORM="linux"
            ;;
        darwin)
            SENTINEL_PLATFORM="darwin"
            ;;
        *)
            SENTINEL_PLATFORM="unknown"
            ;;
    esac

    echo "$SENTINEL_PLATFORM"
}

# ============================================================================
# REFERENCE ID GENERATION
# ============================================================================

# Generate a unique reference ID in format INS-XXXXXX-YYYYMMDD
generate_ref_id() {
    local random_hex
    local date_stamp

    # Generate 6 random hex characters
    if command -v openssl &>/dev/null; then
        random_hex=$(openssl rand -hex 3 | tr '[:lower:]' '[:upper:]')
    elif [[ -r /dev/urandom ]]; then
        random_hex=$(head -c 3 /dev/urandom | xxd -p | tr '[:lower:]' '[:upper:]')
    else
        # Fallback: use timestamp-based random
        random_hex=$(printf '%06X' $((RANDOM * RANDOM % 16777216)))
    fi

    # Get date stamp
    date_stamp=$(date +%Y%m%d)

    # Combine into reference ID
    SENTINEL_REF_ID="INS-${random_hex}-${date_stamp}"
    echo "$SENTINEL_REF_ID"
}

# Get or generate reference ID
get_ref_id() {
    if [[ -z "$SENTINEL_REF_ID" ]]; then
        generate_ref_id >/dev/null
    fi
    echo "$SENTINEL_REF_ID"
}

# ============================================================================
# LOGGING FUNCTIONS
# ============================================================================

# Initialize logging to file
# Usage: init_logging "/var/log/sentinel"
init_logging() {
    local log_dir="${1:-/var/log/sentinel}"

    # Ensure we have a reference ID
    if [[ -z "$SENTINEL_REF_ID" ]]; then
        generate_ref_id >/dev/null
    fi

    # Create log directory
    if ! mkdir -p "$log_dir" 2>/dev/null; then
        # Try temp directory if main fails
        log_dir="/tmp/sentinel"
        mkdir -p "$log_dir" 2>/dev/null || true
    fi

    # Create log file
    SENTINEL_LOG_FILE="${log_dir}/install-${SENTINEL_REF_ID}.log"
    SENTINEL_INSTALL_START_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Write log header
    {
        echo "========================================"
        echo "Sentinel Agent Installer Log"
        echo "Reference ID: $SENTINEL_REF_ID"
        echo "Started: $SENTINEL_INSTALL_START_TIME"
        echo "Platform: $(detect_platform)"
        echo "Architecture: $(uname -m)"
        echo "User: $(whoami)"
        echo "========================================"
        echo ""
    } >> "$SENTINEL_LOG_FILE" 2>/dev/null

    echo "$SENTINEL_LOG_FILE"
}

# Write to log file and optionally stdout
# Usage: write_log "LEVEL" "message"
write_log() {
    local level="$1"
    local message="$2"
    local timestamp
    timestamp=$(date +"%Y-%m-%d %H:%M:%S")

    local log_line="${timestamp} | ${level} | ${message}"

    # Write to log file
    if [[ -n "$SENTINEL_LOG_FILE" ]]; then
        echo "$log_line" >> "$SENTINEL_LOG_FILE" 2>/dev/null
    fi

    # Return log line for callers that want it
    echo "$log_line"
}

# Log debug message
log_debug() {
    local message="$1"
    if [[ "$SENTINEL_LOG_LEVEL" == "DEBUG" ]]; then
        write_log "DEBUG" "$message" >/dev/null
        if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
            echo -e "${COLOR_GRAY}[DEBUG] ${message}${COLOR_NC}"
        fi
    fi
}

# Log info message
log_info() {
    local message="$1"
    write_log "INFO " "$message" >/dev/null
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo -e "${COLOR_GREEN}[+]${COLOR_NC} ${message}"
    fi
}

# Log warning message
log_warn() {
    local message="$1"
    write_log "WARN " "$message" >/dev/null
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo -e "${COLOR_YELLOW}[!]${COLOR_NC} ${message}"
    fi
}

# Log error message
log_error() {
    local message="$1"
    write_log "ERROR" "$message" >/dev/null
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo -e "${COLOR_RED}[X]${COLOR_NC} ${message}" >&2
    fi
}

# Log step/progress message
log_step() {
    local message="$1"
    write_log "INFO " "Step: $message" >/dev/null
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo -e "${COLOR_CYAN}[*]${COLOR_NC} ${message}"
    fi
}

# Log error with error code
log_error_code() {
    local error_code="$1"
    local message="$2"
    local details="${3:-}"

    write_log "ERROR" "[E${error_code}] ${message}" >/dev/null
    if [[ -n "$details" ]]; then
        write_log "ERROR" "  Details: $details" >/dev/null
    fi

    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo -e "${COLOR_RED}[E${error_code}]${COLOR_NC} ${message}" >&2
        if [[ -n "$details" ]]; then
            echo -e "  ${COLOR_GRAY}Details: ${details}${COLOR_NC}" >&2
        fi
    fi
}

# ============================================================================
# ERROR DIALOG FUNCTIONS
# ============================================================================

# Show error dialog (uses GUI on supported platforms)
# Usage: show_error_dialog "Error Title" "Error Message" "Reference ID"
show_error_dialog() {
    local title="${1:-Installation Error}"
    local message="$2"
    local ref_id="${3:-$(get_ref_id)}"

    local full_message="${message}

Reference ID: ${ref_id}
Log file: ${SENTINEL_LOG_FILE:-N/A}

Please contact support with this information."

    case "$SENTINEL_PLATFORM" in
        darwin)
            # macOS: Use osascript for native dialog
            if command -v osascript &>/dev/null; then
                osascript -e "display dialog \"$full_message\" with title \"$title\" buttons {\"OK\"} default button \"OK\" with icon stop" 2>/dev/null || true
            else
                echo -e "${COLOR_RED}ERROR: ${title}${COLOR_NC}"
                echo "$full_message"
            fi
            ;;
        linux)
            # Linux: Try zenity, kdialog, or xmessage
            if command -v zenity &>/dev/null && [[ -n "$DISPLAY" ]]; then
                zenity --error --title="$title" --text="$full_message" --width=400 2>/dev/null || true
            elif command -v kdialog &>/dev/null && [[ -n "$DISPLAY" ]]; then
                kdialog --error "$full_message" --title "$title" 2>/dev/null || true
            elif command -v xmessage &>/dev/null && [[ -n "$DISPLAY" ]]; then
                echo "$full_message" | xmessage -center -title "$title" -file - 2>/dev/null || true
            else
                # Fallback to terminal
                echo -e "${COLOR_RED}========================================${COLOR_NC}"
                echo -e "${COLOR_RED}  ${title}${COLOR_NC}"
                echo -e "${COLOR_RED}========================================${COLOR_NC}"
                echo ""
                echo "$full_message"
            fi
            ;;
        *)
            # Unknown platform: terminal only
            echo -e "${COLOR_RED}ERROR: ${title}${COLOR_NC}"
            echo "$full_message"
            ;;
    esac
}

# Show success dialog
show_success_dialog() {
    local title="${1:-Installation Complete}"
    local message="$2"

    case "$SENTINEL_PLATFORM" in
        darwin)
            if command -v osascript &>/dev/null; then
                osascript -e "display dialog \"$message\" with title \"$title\" buttons {\"OK\"} default button \"OK\" with icon note" 2>/dev/null || true
            else
                echo -e "${COLOR_GREEN}SUCCESS: ${title}${COLOR_NC}"
                echo "$message"
            fi
            ;;
        linux)
            if command -v zenity &>/dev/null && [[ -n "$DISPLAY" ]]; then
                zenity --info --title="$title" --text="$message" --width=400 2>/dev/null || true
            elif command -v kdialog &>/dev/null && [[ -n "$DISPLAY" ]]; then
                kdialog --msgbox "$message" --title "$title" 2>/dev/null || true
            else
                echo -e "${COLOR_GREEN}========================================${COLOR_NC}"
                echo -e "${COLOR_GREEN}  ${title}${COLOR_NC}"
                echo -e "${COLOR_GREEN}========================================${COLOR_NC}"
                echo ""
                echo "$message"
            fi
            ;;
        *)
            echo -e "${COLOR_GREEN}SUCCESS: ${title}${COLOR_NC}"
            echo "$message"
            ;;
    esac
}

# ============================================================================
# ERROR HANDLING
# ============================================================================

# Handle installation error and exit
# Usage: handle_error ERROR_CODE "message" ["details"]
handle_error() {
    local error_code="$1"
    local message="$2"
    local details="${3:-}"
    local ref_id
    ref_id=$(get_ref_id)

    log_error_code "$error_code" "$message" "$details"

    # Write failure marker to log
    if [[ -n "$SENTINEL_LOG_FILE" ]]; then
        {
            echo ""
            echo "========================================"
            echo "INSTALLATION FAILED"
            echo "Error Code: E${error_code}"
            echo "Reference ID: $ref_id"
            echo "Time: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
            echo "========================================"
        } >> "$SENTINEL_LOG_FILE" 2>/dev/null
    fi

    # Show dialog if not in silent mode
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        show_error_dialog "Installation Failed" "[E${error_code}] ${message}" "$ref_id"
    fi

    # Return error code (first digit determines exit code)
    local exit_code=$((error_code / 100))
    exit "$exit_code"
}

# Wrap command execution with error handling
# Usage: run_cmd "description" command arg1 arg2 ...
run_cmd() {
    local description="$1"
    shift

    log_debug "Executing: $*"

    if ! "$@" 2>&1 | while read -r line; do
        log_debug "  $line"
    done; then
        return 1
    fi

    return 0
}

# ============================================================================
# PREREQUISITE CHECKS
# ============================================================================

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        log_info "Try: sudo $0 $*"
        handle_error $E_PERMISSION_DENIED "Administrator privileges required"
    fi
}

# Check for required commands
check_command() {
    local cmd="$1"
    local package="${2:-$cmd}"

    if ! command -v "$cmd" &>/dev/null; then
        log_error "Required command not found: $cmd"
        log_info "Install it with: apt-get install $package (Debian/Ubuntu)"
        log_info "            or: yum install $package (RHEL/CentOS)"
        return 1
    fi
    return 0
}

# Check available disk space
# Usage: check_disk_space /path/to/install 100 (100 MB required)
check_disk_space() {
    local path="$1"
    local required_mb="$2"
    local available_mb

    # Get available space in MB
    if [[ "$SENTINEL_PLATFORM" == "darwin" ]]; then
        available_mb=$(df -m "$path" 2>/dev/null | awk 'NR==2 {print $4}')
    else
        available_mb=$(df -m "$path" 2>/dev/null | awk 'NR==2 {print $4}')
    fi

    if [[ -z "$available_mb" ]]; then
        log_warn "Could not determine available disk space"
        return 0
    fi

    if [[ $available_mb -lt $required_mb ]]; then
        handle_error $E_DISK_SPACE_INSUFFICIENT \
            "Insufficient disk space" \
            "Required: ${required_mb}MB, Available: ${available_mb}MB"
    fi

    log_debug "Disk space check passed: ${available_mb}MB available"
    return 0
}

# ============================================================================
# PROGRESS TRACKING
# ============================================================================

# Progress bar variables
SENTINEL_TOTAL_STEPS=0
SENTINEL_CURRENT_STEP=0

# Initialize progress tracking
# Usage: init_progress 5
init_progress() {
    SENTINEL_TOTAL_STEPS="$1"
    SENTINEL_CURRENT_STEP=0
}

# Update progress
# Usage: update_progress "Installing files..."
update_progress() {
    local message="$1"
    SENTINEL_CURRENT_STEP=$((SENTINEL_CURRENT_STEP + 1))

    local percent=0
    if [[ $SENTINEL_TOTAL_STEPS -gt 0 ]]; then
        percent=$((SENTINEL_CURRENT_STEP * 100 / SENTINEL_TOTAL_STEPS))
    fi

    log_step "[${SENTINEL_CURRENT_STEP}/${SENTINEL_TOTAL_STEPS}] ${message}"

    # Draw progress bar in terminal
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]] && [[ -t 1 ]]; then
        local bar_width=40
        local filled=$((bar_width * percent / 100))
        local empty=$((bar_width - filled))

        printf "\r  ["
        printf "%${filled}s" '' | tr ' ' '='
        printf "%${empty}s" '' | tr ' ' '-'
        printf "] %3d%%" "$percent"

        if [[ $SENTINEL_CURRENT_STEP -eq $SENTINEL_TOTAL_STEPS ]]; then
            echo ""  # New line at end
        fi
    fi
}

# ============================================================================
# CLEANUP
# ============================================================================

# Cleanup handler for trap
cleanup_on_exit() {
    local exit_code=$?

    # Write final log entry
    if [[ -n "$SENTINEL_LOG_FILE" ]]; then
        if [[ $exit_code -eq 0 ]]; then
            echo "Installation completed successfully" >> "$SENTINEL_LOG_FILE"
        else
            echo "Installation failed with exit code: $exit_code" >> "$SENTINEL_LOG_FILE"
        fi
        echo "End time: $(date -u +"%Y-%m-%dT%H:%M:%SZ")" >> "$SENTINEL_LOG_FILE"
    fi
}

# Set up trap for cleanup
setup_cleanup_trap() {
    trap cleanup_on_exit EXIT
}

# ============================================================================
# UTILITY FUNCTIONS
# ============================================================================

# Get error description from code
get_error_description() {
    local code="$1"

    case "$code" in
        100) echo "General installation failure" ;;
        101) echo "Failed to extract installation files" ;;
        102) echo "Insufficient disk space" ;;
        103) echo "Permission denied" ;;
        104) echo "Installation path not found" ;;
        105) echo "Installation path is not writable" ;;
        106) echo "Installation files are in use" ;;
        107) echo "Downloaded binary is corrupted" ;;
        108) echo "File checksum verification failed" ;;
        109) echo "Installer architecture mismatch" ;;
        110) echo "Required system components missing" ;;
        111) echo "Cannot create temporary directory" ;;
        112) echo "Failed to download installer components" ;;
        113) echo "Installation timed out" ;;

        200) echo "General service operation failure" ;;
        201) echo "Failed to create service" ;;
        202) echo "Failed to start service" ;;
        203) echo "Failed to stop service" ;;
        204) echo "Service with same name already exists" ;;
        205) echo "Service does not exist" ;;
        206) echo "Service operation timed out" ;;

        300) echo "General configuration error" ;;
        301) echo "Configuration file is not valid JSON" ;;
        302) echo "Server URL not specified" ;;
        303) echo "Enrollment token not specified" ;;
        304) echo "Failed to write configuration file" ;;

        400) echo "General network error" ;;
        401) echo "Cannot reach Sentinel server" ;;
        402) echo "Enrollment token is invalid" ;;
        403) echo "Enrollment token has expired" ;;
        404) echo "Enrollment token has reached maximum uses" ;;
        405) echo "SSL/TLS certificate verification failed" ;;

        *) echo "Unknown error" ;;
    esac
}

# Print banner
print_banner() {
    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo ""
        echo -e "${COLOR_CYAN}  ____             _   _            _ ${COLOR_NC}"
        echo -e "${COLOR_CYAN} / ___|  ___ _ __ | |_(_)_ __   ___| |${COLOR_NC}"
        echo -e "${COLOR_CYAN} \\___ \\ / _ \\ '_ \\| __| | '_ \\ / _ \\ |${COLOR_NC}"
        echo -e "${COLOR_CYAN}  ___) |  __/ | | | |_| | | | |  __/ |${COLOR_NC}"
        echo -e "${COLOR_CYAN} |____/ \\___|_| |_|\\__|_|_| |_|\\___|_|${COLOR_NC}"
        echo ""
        echo -e "       Remote Monitoring & Management"
        echo ""
    fi
}

# Print completion summary
print_summary() {
    local success="$1"
    local ref_id
    ref_id=$(get_ref_id)

    if [[ "$SENTINEL_SILENT_MODE" != "true" ]]; then
        echo ""
        if [[ "$success" == "true" ]]; then
            echo -e "${COLOR_GREEN}========================================"
            echo -e "  INSTALLATION COMPLETED SUCCESSFULLY"
            echo -e "========================================${COLOR_NC}"
        else
            echo -e "${COLOR_RED}========================================"
            echo -e "  INSTALLATION FAILED"
            echo -e "========================================${COLOR_NC}"
        fi
        echo ""
        echo "  Reference ID: $ref_id"
        echo "  Log file: $SENTINEL_LOG_FILE"
        echo ""
    fi
}

# ============================================================================
# INITIALIZATION
# ============================================================================

# Auto-detect platform on source
detect_platform >/dev/null
