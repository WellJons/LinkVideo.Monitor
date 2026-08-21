#!/bin/bash
set -euo pipefail

APP="/Applications/LinkVideo.Monitor.app"
UNINSTALLER="/Applications/Uninstall LinkVideo Monitor.command"
PACKAGE_ID="ru.linkvideo.monitor.pkg"
PURGE_DATA=0
PRIVILEGED_STAGE=0
USER_HOME="${HOME:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app)
      APP="$2"
      shift 2
      ;;
    --purge-data)
      PURGE_DATA=1
      shift
      ;;
    --privileged-stage)
      PRIVILEGED_STAGE=1
      shift
      ;;
    --user-home)
      USER_HOME="$2"
      shift 2
      ;;
    *)
      echo "Неизвестный аргумент: $1" >&2
      exit 2
      ;;
  esac
done

BIN="$APP/Contents/MacOS/LinkVideo.Monitor"

terminate_tree() {
  local pid="$1"
  local child
  for child in $(/usr/bin/pgrep -P "$pid" 2>/dev/null || true); do
    terminate_tree "$child"
  done
  /bin/kill -TERM "$pid" 2>/dev/null || true
}

stop_installed_processes() {
  local pids pid helper
  pids="$(/usr/bin/pgrep -f "$BIN" 2>/dev/null || true)"
  for pid in $pids; do
    terminate_tree "$pid"
  done

  for helper in \
    "$APP/Contents/Resources/linkvideo-capture-helper" \
    "$APP/Contents/Resources/linkvideo-workspace-helper" \
    "$APP/Contents/Resources/linkvideo-overlay-helper" \
    "$APP/Contents/Resources/linkvideo-hotkey-helper"; do
    for pid in $(/usr/bin/pgrep -f "$helper" 2>/dev/null || true); do
      /bin/kill -TERM "$pid" 2>/dev/null || true
    done
  done

  if [[ -n "$pids" ]]; then
    /bin/sleep 1
  fi

  for pid in $(/usr/bin/pgrep -f "$BIN" 2>/dev/null || true); do
    /bin/kill -KILL "$pid" 2>/dev/null || true
  done
}

unregister_login_item_as_current_user() {
  if [[ ! -x "$BIN" ]]; then
    return 0
  fi
  "$BIN" --set-startup false >/dev/null 2>&1 || {
    echo "Предупреждение: не удалось отключить автозапуск LinkVideo Monitor." >&2
    return 0
  }
}

unregister_login_item_from_root() {
  if [[ ! -x "$BIN" || -z "${SUDO_USER:-}" || "$SUDO_USER" == "root" ]]; then
    return 0
  fi
  local uid
  uid="$(/usr/bin/id -u "$SUDO_USER")"
  /bin/launchctl asuser "$uid" /usr/bin/sudo -u "$SUDO_USER" "$BIN" --set-startup false >/dev/null 2>&1 || {
    echo "Предупреждение: не удалось отключить автозапуск LinkVideo Monitor для $SUDO_USER." >&2
    return 0
  }
}

if [[ "$PRIVILEGED_STAGE" -eq 0 && "$EUID" -ne 0 ]]; then
  unregister_login_item_as_current_user
  stop_installed_processes

  args=("$0" --privileged-stage --app "$APP" --user-home "$USER_HOME")
  if [[ "$PURGE_DATA" -eq 1 ]]; then
    args+=(--purge-data)
  fi
  exec /usr/bin/sudo "${args[@]}"
fi

if [[ "$EUID" -ne 0 ]]; then
  echo "Для удаления LinkVideo Monitor нужны права администратора." >&2
  exit 1
fi

if [[ "$PRIVILEGED_STAGE" -eq 0 ]]; then
  unregister_login_item_from_root
fi
stop_installed_processes

/bin/rm -rf "$APP"
/usr/sbin/pkgutil --forget "$PACKAGE_ID" >/dev/null 2>&1 || true

if [[ "$PURGE_DATA" -eq 1 && -n "$USER_HOME" && "$USER_HOME" != "/" ]]; then
  /bin/rm -rf \
    "$USER_HOME/Library/Application Support/LinkVideo.Monitor" \
    "$USER_HOME/Library/Caches/LinkVideo.Monitor"
fi

# The PKG installs this command next to the application so removal remains
# available after the original installer image has been ejected.
if [[ "$UNINSTALLER" != "$0" ]]; then
  /bin/rm -f "$UNINSTALLER"
else
  /bin/rm -f "$0"
fi

echo "LinkVideo Monitor удалён."
if [[ "$PURGE_DATA" -eq 0 ]]; then
  echo "Настройки и журналы сохранены. Для полного удаления используйте --purge-data."
fi
