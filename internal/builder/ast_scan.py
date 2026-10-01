# -*- coding: utf-8 -*-
"""资源依赖静态分析脚本（AST 语法树方案，替代正则）。

只分析真正执行"文件读取"的调用节点，不碰注释、打印文本、后缀判断等噪音。
输出 JSON 数组：
  [{ref, file, line, dynamic, kind, file_base}]
  - ref      提取到的路径字符串（dynamic 时为 null）
  - file     相对程序根目录的源文件
  - line     行号（AST 节点自带，精确）
  - dynamic  参数是变量而非字符串常量（无法静态识别路径）
  - kind     open / join / load / dll / copy 等调用类型
  - file_base 路径基于 __file__ 所在源文件目录解析
"""
import ast
import json
import os
import sys

# 双保险：强制 stdout 以 UTF-8 输出，避免 Windows 下 GBK 编码导致中文乱码
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass

# 分析时跳过的目录（缓存 / 虚拟环境 / 构建产物）
SKIP_DIRS = {
    '__pycache__', '.venv', 'venv', 'env', 'build', 'dist', '.git',
    'node_modules', '.idea', '.vscode', '.pytest_cache',
}

# 读取类调用：第一个参数是"被读取的文件"（open 内置 / cv2.imread / np.load /
# torch.load / pd.read_csv / Image.open / shutil.copy 等）
READ_CALLS = {
    'open', 'imread', 'imdecode', 'load', 'load_weights', 'load_model',
    'read_csv', 'read_excel', 'read_json', 'read_pickle', 'read_hdf',
    'read_sql', 'read_sql_table', 'read_sql_query', 'read_table', 'read_fwf',
    'read_parquet', 'read_feather', 'read_orc', 'read_sas', 'read_spss',
    'read_stata', 'read_xml', 'read_html', 'read_clipboard', 'read_gbq',
    'from_file', 'from_pickle', 'from_bytes', 'copy', 'copy2', 'copyfile',
    'copyfileobj', 'move', 'send_file', 'load_image', 'read_image',
    'read_text', 'read_bytes', 'read', 'readlines',
}

# DLL / 动态库加载调用（ctypes / ctypes.windll / LoadLibrary 等）
DLL_CALLS = {
    'WinDLL', 'CDLL', 'OleDLL', 'PyDLL', 'windll', 'oledll', 'pydll',
    'LoadLibrary', 'LoadLibraryEx', 'LoadPackagedLibrary', 'GetProcAddress',
}


def _name_of(func):
    """取调用函数名：内置 open → 'open'；cv2.imread → 'imread'；path.join → 'join'"""
    if isinstance(func, ast.Name):
        return func.id
    if isinstance(func, ast.Attribute):
        return func.attr
    return None


def _contains_file(node):
    """调用链里是否出现 __file__（用于判断路径基准）"""
    if isinstance(node, ast.Name) and node.id == '__file__':
        return True
    if isinstance(node, ast.Call):
        for a in node.args:
            if _contains_file(a):
                return True
        for kw in node.keywords:
            if kw.value and _contains_file(kw.value):
                return True
    return False


# 常见资源扩展名（赋值语句兜底判断用）
RES_EXTS = {
    '.png', '.jpg', '.jpeg', '.gif', '.bmp', '.ico', '.svg', '.webp', '.tif', '.tiff',
    '.json', '.yaml', '.yml', '.txt', '.csv', '.xml', '.ini', '.conf', '.cfg', '.toml',
    '.xlsx', '.xls', '.doc', '.docx', '.pdf', '.zip', '.7z', '.rar', '.gz', '.tar',
    '.wav', '.mp3', '.mp4', '.avi', '.mkv', '.mov', '.flv', '.ttf', '.otf',
    '.db', '.sqlite', '.dat', '.bin', '.pkl', '.npy', '.npz', '.h5', '.hdf5',
    '.onnx', '.pt', '.pth', '.weights', '.engine', '.dll', '.pyd', '.pem', '.crt', '.key',
}


def scan_file(path, root):
    try:
        with open(path, 'r', encoding='utf-8', errors='ignore') as f:
            lines = f.read().splitlines()
        tree = ast.parse('\n'.join(lines))
    except Exception:
        return []
    rel = os.path.relpath(path, root).replace('\\', '/')

    def code_of(node):
        """取引用所在行的源码文本（去掉首尾空白），供界面展示依据代码"""
        ln = getattr(node, 'lineno', 0)
        if 1 <= ln <= len(lines):
            return lines[ln - 1].strip()[:200]
        return ''

    out = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        name = _name_of(node.func)
        if name is None:
            continue
        line = getattr(node, 'lineno', 0)

        # 1. DLL 加载：ctypes.WinDLL('user32.dll') / LoadLibrary('x.dll')
        if name in DLL_CALLS:
            arg = node.args[0] if node.args else None
            if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                out.append({'ref': arg.value, 'file': rel, 'line': line,
                            'dynamic': False, 'kind': 'dll', 'file_base': False,
                            'code': code_of(node)})
            elif arg is not None:
                out.append({'ref': None, 'file': rel, 'line': line,
                            'dynamic': True, 'kind': 'dll', 'file_base': False,
                            'code': code_of(node)})
            continue

        # 2. os.path.join / pathlib Path 拼接：字符串常量参数按顺序合并
        if name == 'join' and isinstance(node.func, ast.Attribute):
            parts = []
            file_base = False
            for arg in node.args:
                if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                    parts.append(arg.value)
                elif _contains_file(arg):
                    file_base = True
            if parts:
                out.append({'ref': '/'.join(parts), 'file': rel, 'line': line,
                            'dynamic': False, 'kind': 'join', 'file_base': file_base,
                            'code': code_of(node)})
            continue

        # 3. pathlib.Path('xxx') 单参数构造
        if name == 'Path' and isinstance(node.func, ast.Name):
            arg = node.args[0] if node.args else None
            if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                out.append({'ref': arg.value, 'file': rel, 'line': line,
                            'dynamic': False, 'kind': 'path', 'file_base': False,
                            'code': code_of(node)})
            continue

        # 4. 读取类调用：第一个字符串常量参数是资源路径
        if name in READ_CALLS:
            arg = node.args[0] if node.args else None
            if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                out.append({'ref': arg.value, 'file': rel, 'line': line,
                            'dynamic': False, 'kind': name, 'file_base': False,
                            'code': code_of(node)})
            elif arg is not None:
                out.append({'ref': None, 'file': rel, 'line': line,
                            'dynamic': True, 'kind': name, 'file_base': False,
                            'code': code_of(node)})
            continue
    # 5. 字符串常量赋值（如 图标文件名 = 'logo.png'）：变量可能后续用于读取。
    #    仅当值以常见资源扩展名结尾才提取，避免普通文本误报。
    for node in ast.walk(tree):
        if isinstance(node, ast.Assign) and isinstance(node.value, ast.Constant) \
                and isinstance(node.value.value, str):
            val = node.value.value
            if os.path.splitext(val)[1].lower() in RES_EXTS:
                out.append({'ref': val, 'file': rel,
                            'line': getattr(node, 'lineno', 0),
                            'dynamic': False, 'kind': 'assign', 'file_base': False,
                            'code': code_of(node)})
    return out


def main():
    root = sys.argv[1]
    results = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for fn in filenames:
            if fn.lower().endswith('.py'):
                results.extend(scan_file(os.path.join(dirpath, fn), root))
    print(json.dumps(results, ensure_ascii=False))


if __name__ == '__main__':
    main()
