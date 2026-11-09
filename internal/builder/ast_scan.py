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
import tokenize

# 双保险：强制 stdout 以 UTF-8 输出，避免 Windows 下 GBK 编码导致中文乱码
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass

# 兜底清单：正常由 Go 侧通过第二个参数传入（internal/fsx 是唯一出处，避免两边走样），
# 这里只用于手工单独运行本脚本的场景。
SKIP_DIRS = {
    '__pycache__', '.venv', 'venv', 'venv_py', 'env', 'build', 'dist', '.git',
    'node_modules', '.idea', '.vscode', '.pytest_cache',
}


def _is_venv(dirpath):
    """Python 3.3+ 创建的虚拟环境根目录必有 pyvenv.cfg 标志文件。

    用文件识别比枚举目录名更可靠：venv_py / venv312 / _venv / myenv 等
    任意命名都能跳过，不会再把第三方库内部代码当成程序资源依赖。
    """
    return os.path.isfile(os.path.join(dirpath, 'pyvenv.cfg'))

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
    """表达式里是否出现 __file__（用于判断路径基准）"""
    if isinstance(node, ast.Name) and node.id == '__file__':
        return True
    if isinstance(node, ast.Attribute):
        # Path(__file__).parent / Path(__file__).parent
        return _contains_file(node.value)
    if isinstance(node, ast.BinOp):
        return _contains_file(node.left) or _contains_file(node.right)
    if isinstance(node, ast.Call):
        for a in node.args:
            if _contains_file(a):
                return True
        for kw in node.keywords:
            if kw.value and _contains_file(kw.value):
                return True
    return False


def _path_parts(node):
    """把 pathlib 的 / 拼接拆成 (路径片段列表, 是否基于 __file__)。

    现代写法普遍是 Path(__file__).parent / 'assets' / 'logo.png'，
    这是 BinOp(Div) 而不是函数调用，只按调用名匹配会整条漏掉。
    """
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Div):
        lp, lf = _path_parts(node.left)
        rp, rf = _path_parts(node.right)
        return lp + rp, lf or rf
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return [node.value], False
    if _contains_file(node):
        return [], True
    return [], False


def _is_write_mode(node):
    """open() 的写入模式判断（open('out.txt', 'w') 不是资源依赖）。

    第二个位置参数或 mode 关键字可能写着模式串；只认字符串常量，
    变量一律按"读"处理（宁可多报，不漏报）。
    """
    mode = None
    if len(node.args) >= 2 and isinstance(node.args[1], ast.Constant) \
            and isinstance(node.args[1].value, str):
        mode = node.args[1].value
    for kw in node.keywords:
        if kw.arg == 'mode' and isinstance(kw.value, ast.Constant) \
                and isinstance(kw.value.value, str):
            mode = kw.value.value
    return bool(mode) and any(c in mode for c in 'wax')


# 常见资源扩展名（赋值语句/任意调用兜底判断用）。
# 覆盖桌面程序真正会随包分发的文件：界面（qss/ui/qrc）、网页（html/css/js）、
# 模型权重、字体、数据库、配置、素材等。
RES_EXTS = {
    # 图片
    '.png', '.jpg', '.jpeg', '.gif', '.bmp', '.ico', '.icns', '.cur', '.svg', '.webp',
    '.tif', '.tiff', '.psd', '.tga',
    # 界面 / 网页（PySide/PyQt 项目高频）
    '.qss', '.qrc', '.ui', '.html', '.htm', '.css', '.js', '.map',
    # 配置 / 数据
    '.json', '.json5', '.yaml', '.yml', '.txt', '.csv', '.tsv', '.xml', '.ini', '.conf',
    '.cfg', '.toml', '.env', '.properties', '.sql', '.db', '.sqlite', '.sqlite3', '.dat', '.bin',
    # 文档 / 压缩
    '.xlsx', '.xls', '.doc', '.docx', '.pdf', '.zip', '.7z', '.rar', '.gz', '.tar', '.bz2',
    # 音视频 / 字幕
    '.wav', '.mp3', '.ogg', '.flac', '.m4a', '.aac', '.mp4', '.avi', '.mkv', '.mov',
    '.flv', '.webm', '.srt', '.vtt',
    # 字体
    '.ttf', '.otf', '.ttc', '.woff', '.woff2', '.eot',
    # 模型 / 科学计算
    '.pkl', '.pickle', '.joblib', '.npy', '.npz', '.h5', '.hdf5', '.onnx', '.pt', '.pth',
    '.ckpt', '.safetensors', '.tflite', '.pb', '.pbtxt', '.mat', '.arff', '.msgpack',
    '.weights', '.engine', '.gltf', '.glb', '.obj', '.fbx', '.stl',
    # 本地化 / 二进制
    '.mo', '.po', '.qm', '.dll', '.pyd', '.pem', '.crt', '.key',
}

def _has_res_ext(s):
    """字符串是否带"资源文件"扩展名（且不是 URL）。

    这是"任何函数调用/常量赋值"这两条兜底规则的准入条件：
    只靠"带目录分隔符"会把正则、URL 路径段、日志文案一起捞进来。
    """
    if not s or len(s) > 400:
        return False
    if '://' in s:
        return False  # URL 不是随包分发的资源
    if s.startswith(':') or s.startswith('qrc:'):
        return False  # Qt 资源系统里的伪路径（:/icons/a.png），不是磁盘文件
    if any(c in s for c in '*?['):
        return False  # glob 模式描述一批文件，不是某个具体资源
    return os.path.splitext(s)[1].lower() in RES_EXTS


def scan_file(path, root):
    try:
        # tokenize.open 会读源码里的编码声明（PEP 263）：
        # Windows 老项目常见 `# -*- coding: gbk -*-`，按 UTF-8 硬读会让中文路径变成乱码。
        with tokenize.open(path) as f:
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
        line = getattr(node, 'lineno', 0)

        # 0. pathlib 的 / 拼接：Path(__file__).parent / 'assets' / 'logo.png'
        #    这是 BinOp 而不是函数调用，必须放在"不是 Call 就跳过"之前判断。
        if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Div):
            parts, file_base = _path_parts(node)
            if parts:
                out.append({'ref': '/'.join(parts), 'file': rel, 'line': line,
                            'dynamic': False, 'kind': 'join', 'file_base': file_base,
                            'code': code_of(node)})
            continue

        if not isinstance(node, ast.Call):
            continue
        name = _name_of(node.func)
        if name is None:
            continue

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

        # 3. pathlib.Path('xxx') 单参数构造（Path(...) 与 pathlib.Path(...) 都算）
        if name == 'Path':
            arg = node.args[0] if node.args else None
            if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                out.append({'ref': arg.value, 'file': rel, 'line': line,
                            'dynamic': False, 'kind': 'path', 'file_base': False,
                            'code': code_of(node)})
            continue

        # 4. 读取类调用：第一个字符串常量参数是资源路径
        if name in READ_CALLS:
            if name == 'open' and _is_write_mode(node):
                continue  # 写文件不是"必需资源"，报出来只会误导
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
    # 5. 任意位置的字符串字面量（赋值、字典/列表、函数参数与默认值……）。
    #
    # 为什么不再按"赋值"或"特定函数名"匹配：资源路径在真实代码里出现在任何地方——
    #   QIcon('assets/icon.png')、resource_path('data/config.json')、
    #   MENU = [{'icon': 'a.png'}]、add_argument(default='cfg.json')
    # 函数名不可枚举、位置也不固定，白名单注定漏。这里只按"带资源扩展名"从宽收候选，
    # 精度交给 Go 侧：文件不存在、没勾选、又不带目录的裸名字会被丢掉。
    #
    # 两类噪音要提前排除：文档字符串（说明文字里常出现文件名）和 f-string 的片段
    # （f'{BASE}/assets/a.png' 的常量段只有 "/assets/a.png"，单独解析会得到错误路径）。
    docstring_ids = set()
    for n in ast.walk(tree):
        if isinstance(n, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            body = getattr(n, 'body', None)
            if body and isinstance(body[0], ast.Expr) \
                    and isinstance(body[0].value, ast.Constant) \
                    and isinstance(body[0].value.value, str):
                docstring_ids.add(id(body[0].value))
    fstring_ids = set()
    for n in ast.walk(tree):
        if isinstance(n, ast.JoinedStr):
            for v in n.values:
                if isinstance(v, ast.Constant):
                    fstring_ids.add(id(v))

    # 父节点索引：用来判断某个字面量是不是已经被上面的结构化分支处理过
    # （os.path.join 的片段、pathlib 的 / 片段、open/read_* 的参数），
    # 否则 'logo.png' 会作为 'assets/logo.png' 的碎片再报一条，界面出现重复项。
    parents = {}
    for n in ast.walk(tree):
        for child in ast.iter_child_nodes(n):
            parents[id(child)] = n

    def handled_structurally(node):
        p = parents.get(id(node))
        if isinstance(p, ast.BinOp) and isinstance(p.op, ast.Div):
            return True
        if isinstance(p, ast.Call):
            name = _name_of(p.func)
            if name in ('join', 'Path') or name in READ_CALLS or name in DLL_CALLS:
                return True
        return False

    for node in ast.walk(tree):
        if not isinstance(node, ast.Constant) or not isinstance(node.value, str):
            continue
        if id(node) in docstring_ids or id(node) in fstring_ids:
            continue
        if handled_structurally(node):
            continue
        val = node.value
        if not _has_res_ext(val):
            continue
        if any(ch.isspace() for ch in val):
            continue  # 带空格的几乎都是提示文案，不是路径
        out.append({'ref': val, 'file': rel,
                    'line': getattr(node, 'lineno', 0),
                    'dynamic': False, 'kind': 'literal', 'file_base': False,
                    'code': code_of(node)})
    return out


def main():
    root = sys.argv[1]
    skip_dirs = {d.lower() for d in SKIP_DIRS}
    if len(sys.argv) > 2 and sys.argv[2]:
        try:
            skip_dirs = {str(d).lower() for d in json.loads(sys.argv[2])}
        except Exception:
            pass
    results = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames
                       if d.lower() not in skip_dirs and not _is_venv(os.path.join(dirpath, d))]
        for fn in filenames:
            if fn.lower().endswith('.py'):
                results.extend(scan_file(os.path.join(dirpath, fn), root))
    print(json.dumps(results, ensure_ascii=False))


if __name__ == '__main__':
    main()
