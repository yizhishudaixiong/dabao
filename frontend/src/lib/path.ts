// 通用路径工具：多个页面共用的路径解析，避免各自重复实现
// （Windows 路径可能同时含 \ 和 /，统一取最后一级/目录部分）

// baseName 取路径最后一级（文件名或文件夹名）
export function baseName(p: string): string {
  const idx = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return idx >= 0 ? p.slice(idx + 1) : p
}

// dirOf 取路径的目录部分
export function dirOf(p: string): string {
  const idx = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return idx >= 0 ? p.slice(0, idx) : p
}
