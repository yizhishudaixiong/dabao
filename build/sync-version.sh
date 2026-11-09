#!/usr/bin/env bash
# 把 internal/version/version.go 里的版本号同步到 build/windows/info.json。
#
# 为什么单独成脚本，而不是写在流水线的 YAML 里：
#   码云 Gitee Go 会把 commands 列表拼成一个 COMMAND.sh 再执行，这个过程中
#   YAML 单引号内反斜杠转义的引号（\"）会被折叠成普通引号，sed 表达式里的
#   引号因此提前闭合，报 `syntax error near unexpected token '"product_version":'`。
#   逻辑放进脚本文件后，引号不再经过 YAML 解析；GitHub Actions 也复用同一份实现。
#
# 用法：bash build/sync-version.sh
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ver_file="$root/internal/version/version.go"
info_file="$root/build/windows/info.json"

# 不用 grep -oP：BSD/macOS 的 grep 没有 -P，本脚本在本地 Git Bash 也要能跑
ver="$(sed -n 's/.*[^A-Za-z]Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$ver_file" | head -n 1)"
if [ -z "$ver" ]; then
  echo "无法从 $ver_file 读取版本号" >&2
  exit 1
fi
# Windows 文件版本是四段：version.go 里的 6.3.0 -> 6.3.0.0
full="$ver.0"

# info.json 里有两组版本字段，必须一起改：
#   fixed.file_version / fixed.product_version   —— 数字资源
#   info.0000.FileVersion / ProductVersion       —— 字符串资源
# 只改一组时 exe 属性里会显示旧版本号（踩过一次）。
for key in file_version product_version FileVersion ProductVersion; do
  sed -i.bak -E "s/(\"${key}\"[[:space:]]*:[[:space:]]*\")[^\"]*\"/\1${full}\"/" "$info_file"
done
rm -f "$info_file.bak"

# 校验四个字段都写进去了：避免"命令跑过了但没生效"的静默失败
for key in file_version product_version FileVersion ProductVersion; do
  if ! grep -q "\"${key}\"[[:space:]]*:[[:space:]]*\"${full}\"" "$info_file"; then
    echo "同步失败：$info_file 中的 $key 不是 $full" >&2
    cat "$info_file" >&2
    exit 1
  fi
done

echo "已同步 info.json 版本 -> $full"
cat "$info_file"
