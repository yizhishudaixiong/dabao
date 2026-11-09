# 依赖分析脚本：由 Go 侧 go:embed 读取，写入临时文件后用目标 Python 解释器执行。
# 调用约定：argv[1] = 入口脚本路径，argv[2] = 分析缓存目录；结果以单行 JSON 输出到 stdout。
# 原理与字段说明见 internal/deps/scan.go 中 Analyze 的注释。
import ast, json, os, re, sys, time, tokenize, hashlib, importlib.metadata, importlib.util

# 强制 UTF-8 输出，保证中文包名不乱码
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass

def is_stdlib(name):
    try:
        return name in sys.stdlib_module_names
    except AttributeError:
        return False

def read_source(path):
    """按源码自己声明的编码读取（PEP 263 / BOM）。

    不能固定用 utf-8：Windows 上的老项目常见 `# -*- coding: gbk -*-`，
    按 utf-8 硬读会把中文路径、中文模块名读成乱码，import 名与资源路径随之失真。
    tokenize.open 会解析编码声明，是标准库给出的正确做法。
    """
    with tokenize.open(path) as f:
        return f.read()

def under(path, root):
    """判断 path 是否位于 root 之内（按路径层级）。

    不能用 str.startswith：C:\\proj2\\x.py 会被误判成 C:\\proj 的子文件。
    """
    try:
        p = os.path.abspath(path)
        r = os.path.abspath(root)
        return os.path.commonpath([p, r]) == r
    except (ValueError, OSError):
        return False

# 依赖扫描要跳过的目录/文件：测试、文档、示例里的 import 不是程序运行所需，
# 算进来会凭空多出 pytest / sphinx 之类的"缺失依赖"。
# 注意不能按名字跳 env / venv 之类可能被用户拿来当包名的目录，
# 真正的虚拟环境一律有 pyvenv.cfg，用标志文件识别。
SKIP_SCAN_DIRS = {
    '__pycache__', '.git', '.svn', '.hg', '.venv', 'venv', 'venv_py',
    'site-packages', 'node_modules', '.idea', '.vscode', '.pytest_cache',
    'build', 'dist', 'tests', 'test', 'docs', 'doc', 'examples', 'example',
    'benchmarks', 'bench',
}
SKIP_SCAN_FILES = {'setup.py', 'conftest.py', 'noxfile.py', 'tasks.py'}


def iter_project_py(root):
    """遍历项目目录下所有 .py（跳过噪音 / 测试 / 文档目录与构建脚本）。

    为什么不只扫 PyInstaller 收集到的文件：sys.path.append('lib') 这类注入目录、
    插件式加载的目录，PyInstaller 都看不到，那些文件里的 import 就会整片漏掉。
    """
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            d for d in dirnames
            if d.lower() not in SKIP_SCAN_DIRS
            and not os.path.isfile(os.path.join(dirpath, d, 'pyvenv.cfg'))
        ]
        for fn in filenames:
            if fn.lower().endswith('.py') and fn.lower() not in SKIP_SCAN_FILES:
                yield os.path.join(dirpath, fn)


def dir_has_py(root, max_entries=300):
    """目录里是否有 Python 文件（最多看 max_entries 个文件就下结论）。

    只用于判断顶层目录是不是命名空间包；对 assets/ 这类只有图片的大目录，
    走完整棵树纯属浪费，所以给了上限（超限按"没有"处理，不影响主流程）。
    """
    seen = 0
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            d for d in dirnames
            if d.lower() not in SKIP_SCAN_DIRS
            and not os.path.isfile(os.path.join(dirpath, d, 'pyvenv.cfg'))
        ]
        for fn in filenames:
            if fn.lower().endswith('.py') and fn.lower() not in SKIP_SCAN_FILES:
                return True
            seen += 1
            if seen > max_entries:
                return False
    return False


def source_signature(root):
    """项目源码指纹：所有 .py 的 相对路径 + 大小 + 修改时间。

    为什么不能只靠 PyInstaller 自己的缓存判断：它只比对"上次收集到的文件"的 mtime。
    sys.path 注入目录、插件目录里的文件根本不在依赖图里，改了它们不会让缓存失效，
    重新分析会拿到旧结果（正好是这类项目最容易出问题的地方）。
    """
    parts = []
    for p in iter_project_py(root):
        try:
            st = os.stat(p)
        except OSError:
            continue
        try:
            rel = os.path.relpath(p, root)
        except ValueError:
            rel = p
        parts.append('%s:%d:%d' % (rel.replace('\\', '/'), st.st_size, st.st_mtime_ns))
    parts.sort()
    return hashlib.md5('\n'.join(parts).encode('utf-8')).hexdigest()[:12]


def env_signature():
    """当前环境的"已装包指纹"：所有发行版的 名称==版本 排序后取哈希。

    PyInstaller 的分析结果（Analysis-00.toc）只按"上次收集到的文件 mtime"判断能否复用，
    新装的包不在上次的清单里，不会让缓存失效。表现是：先分析报某包缺失 → 装上 →
    再分析仍拿着旧清单，analysisModules 里没有这个包，加密打包就不会收集它。
    把指纹放进缓存目录名，环境一变就自动重新分析，环境不变仍然秒回。
    """
    parts = []
    for d in importlib.metadata.distributions():
        try:
            n = (d.metadata.get('Name') or '').strip().lower()
            v = d.version or ''
        except Exception:
            continue
        if n:
            parts.append('%s==%s' % (n, v))
    parts.sort()
    return hashlib.md5('\n'.join(parts).encode('utf-8')).hexdigest()[:12]

def norm(name):
    # 归一化包名用于匹配（忽略大小写、连字符/下划线差异）
    return name.lower().replace('-', '').replace('_', '')

def parse_mapping(raw):
    """解析 pipreqs 官方映射表（每行 模块名:包名）：
    - key 统一转小写（import PIL 和 import pil 都能命中）
    - 包名统一转小写 + 下划线转连字符（scikit_learn -> scikit-learn，
      Requests -> requests，pip 安装时大小写不敏感，小写为规范形式）
    """
    m = {}
    for line in raw.splitlines():
        line = line.strip()
        if not line or ':' not in line:
            continue
        mod, pkg = line.split(':', 1)
        mod = mod.strip().lower()
        pkg = pkg.strip().replace('_', '-').lower()
        if mod and pkg:
            m[mod] = pkg
    return m

# 官方映射表（pipreqs 0.5.0 自带，1152 条，由 Go 侧 go:embed 注入）
OFFICIAL_RAW = r'''__PIPREQS_MAPPING__'''
MODULE_TO_PKG = parse_mapping(OFFICIAL_RAW)

# 官方表缺失的补充（Windows 专属与常见遗漏）：
MODULE_TO_PKG.update({
    'cv2': 'opencv-python',
    'win32api': 'pywin32', 'win32gui': 'pywin32', 'win32process': 'pywin32',
    'win32com': 'pywin32', 'win32con': 'pywin32', 'win32event': 'pywin32',
    'win32file': 'pywin32', 'win32pipe': 'pywin32', 'win32security': 'pywin32',
    'win32service': 'pywin32', 'win32ts': 'pywin32', 'win32clipboard': 'pywin32',
    'win32evtlog': 'pywin32', 'win32net': 'pywin32', 'win32print': 'pywin32',
    'win32ui': 'pywin32', 'pywintypes': 'pywin32', 'pythoncom': 'pywin32',
    'skimage': 'scikit-image',
    'nacl': 'pynacl',
    'pkg_resources': 'setuptools',
    # pipreqs 官方表遗漏、但国内桌面/自动化项目高频的模块。
    # 漏了会退化成"用模块名当包名"，用户装到的往往是同名野包或装不上。
    'fitz': 'PyMuPDF',          # PDF 处理
    'docx': 'python-docx',      # Word
    'pptx': 'python-pptx',      # PPT
    'grpc': 'grpcio',           # 注意 PyPI 上另有一个废弃的 grpc 包
    'wx': 'wxPython',
    'paddle': 'paddlepaddle',
    'gi': 'PyGObject',
    'clr': 'pythonnet',         # .NET 互操作
    'websocket': 'websocket-client',
    'socketio': 'python-socketio',
    'engineio': 'python-engineio',
    'speech_recognition': 'SpeechRecognition',
    'jose': 'python-jose',
    'multipart': 'python-multipart',
    'dns': 'dnspython',
    'socks': 'PySocks',
    'ldap': 'python-ldap',
    # pipreqs 把 MySQLdb 映射到 MySQL-python（只支持 Python 2），必须覆盖。
    # 注意 key 必须小写：查找用的是 mod.lower()
    'mysqldb': 'mysqlclient',
})

def parse_req(path):
    """解析 requirements.txt，返回 {归一化包名: 版本约束}，如 {'numpy': '==1.26.0'}"""
    constraints = {}
    try:
        with open(path, 'r', encoding='utf-8', errors='ignore') as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                if '#' in line:
                    line = line.split('#', 1)[0].strip()
                if not line:
                    continue
                if line.startswith(('-r', '-e', '-c', '--')):
                    continue
                line = re.sub(r'\[[^\]]*\]', '', line)
                m = re.search(r'(==|>=|<=|!=|~=|<|>)', line)
                if m:
                    name, ver = line[:m.start()].strip(), line[m.start():].strip()
                    constraints[norm(name)] = ver
                else:
                    constraints[norm(line.strip())] = ''
    except Exception:
        pass
    return constraints

def main(entry, workpath):
    entry = os.path.abspath(entry)
    base_dir = os.path.dirname(entry)
    t0 = time.time()
    # 读取同目录 requirements.txt 的版本约束（没有该文件则无约束，装最新版）
    constraints = parse_req(os.path.join(base_dir, 'requirements.txt'))

    # ---------- 1. PyInstaller 官方分析引擎（与正式打包同一套） ----------
    # workpath 按项目分目录（Go 侧传入）：PyInstaller 自带"结果未变则复用缓存"，
    # 重复分析（如加密打包时）秒回；缓存按项目隔离，切项目不互相覆盖。
    from PyInstaller.config import CONF
    from PyInstaller.building.build_main import Analysis
    spec = os.path.join(workpath, 'deps.spec')
    CONF['spec'] = spec
    CONF['specpath'], CONF['specnm'] = os.path.split(spec)
    CONF['specnm'] = os.path.splitext(CONF['specnm'])[0]
    CONF['distpath'] = os.path.join(workpath, 'dist')
    CONF['workpath'] = os.path.join(workpath, 'build')
    CONF['warnfile'] = os.path.join(CONF['workpath'], 'warn-%s.txt' % CONF['specnm'])
    CONF['dot-file'] = os.path.join(CONF['workpath'], 'graph-%s.dot' % CONF['specnm'])
    CONF['xref-file'] = os.path.join(CONF['workpath'], 'xref-%s.html' % CONF['specnm'])
    CONF['code_cache'] = dict()
    os.makedirs(CONF['workpath'], exist_ok=True)
    os.makedirs(CONF['distpath'], exist_ok=True)
    # 上次的分析结果是否存在（PyInstaller 会复用 build 目录下的 Analysis-00.toc）
    toc_file = os.path.join(CONF['workpath'], 'Analysis-00.toc')
    had_cache = os.path.isfile(toc_file)

    # PyInstaller 的缓存只比对"上次收集到的文件"的 mtime，两类变化它都看不见：
    #   ① 新装/卸载/升级了包（新包不在旧清单里）；
    #   ② 改了不在依赖图里的本地文件（sys.path 注入目录、插件目录）。
    # 两份指纹拼成一个"戳"，与上次不同就删掉 Analysis-00.toc 强制重新分析。
    stamp_file = os.path.join(workpath, 'analyze-stamp.txt')
    stamp = env_signature() + '-' + source_signature(base_dir)
    try:
        with open(stamp_file, 'r', encoding='utf-8') as f:
            prev_stamp = f.read().strip()
    except OSError:
        prev_stamp = ''
    if prev_stamp != stamp and os.path.isfile(toc_file):
        try:
            os.remove(toc_file)
            had_cache = False
        except OSError:
            pass

    a = Analysis([entry], pathex=[base_dir], binaries=[], datas=[],
                 hiddenimports=[], hookspath=[], runtime_hooks=[],
                 excludes=[], noarchive=False)
    dt = time.time() - t0
    # 只有"上次结果存在 + 本次很快返回"才算命中缓存；
    # 单看耗时会把小型项目的首次分析（引擎加载本身也要几秒）误标成命中
    is_cached = had_cache and dt < 3.0
    try:
        with open(stamp_file, 'w', encoding='utf-8') as f:
            f.write(stamp)
    except OSError:
        pass

    # ---------- 2. 已收集模块集合 ----------
    # 注意：analysisModules 只使用纯 Python 模块（pure）的模块名；
    # binaries 的 name 是收集目标路径（如 "win32/win32gui.pyd"），不适合当 --hidden-import，
    # 扩展模块由 --collect-all 包名 + 顶层 hidden-import 覆盖。
    collected = {}
    pure_names = set()
    for name, path, tc in a.pure:
        collected[name] = ('pure', path)
        pure_names.add(name)
    for name, path, tc in a.binaries:
        collected[name] = ('bin', path)

    def is_site_packages(path):
        # 第三方库统一装在 site-packages（Windows/macOS）或 dist-packages（Linux）下，
        # 路径命中即视为第三方库文件，不是用户自己的本地模块
        p = str(path).replace('\\', '/').lower()
        return '/site-packages/' in p or '/dist-packages/' in p

    # 项目里真实存在的模块名（顶层名）：用来判断某个 import 到底是本地代码还是第三方。
    # 只看 PyInstaller 收集结果是不够的——sys.path 注入目录里的本地模块不会被收集，
    # 于是 import helpers 会被当成"缺失的 pip 包 helpers"，用户一装就装进一个同名野包。
    local_names = set()
    for p in iter_project_py(base_dir):
        stem = os.path.splitext(os.path.basename(p))[0]
        if stem != '__init__':
            local_names.add(stem)
    # 顶层目录里的"命名空间包"（PEP 420，没有 __init__.py 但有 .py）：
    # import ns.thing 里的 ns 同样是本地代码，不能报成缺失的 pip 包。
    try:
        for entry in os.scandir(base_dir):
            if not entry.is_dir():
                continue
            if entry.name.lower() in SKIP_SCAN_DIRS:
                continue
            if dir_has_py(entry.path):
                local_names.add(entry.name)
    except OSError:
        pass

    def is_local_mod(mod):
        # ① PyInstaller 收集到的、位于项目目录内的模块（最权威）
        info = collected.get(mod)
        if info:
            path = info[1] or ''
            if path and not is_site_packages(path) and under(path, base_dir):
                return True
        # ② 项目目录里存在同名 .py（sys.path 注入 / 插件目录里的本地模块）
        return mod in local_names

    def is_third_party(name, path):
        # 第三方：路径在 site-packages 里，或本身就是 .pyd 扩展模块
        if not path:
            return False
        p = str(path).replace('\\', '/')
        return 'site-packages' in p or p.endswith('.pyd')

    # ---------- 3. 候选提取 ----------
    # 本地模块发现交给 PyInstaller：它的收集结果里已经包含"被 import 链上的全部本地文件"
    # （含包内相对导入 from . import xxx 的模块），比我们手写递归更全更省事。
    # 对这些本地文件做 AST 提取 import 名，并记录来源文件与行号（供界面展示）。
    local_files = set()
    for name, info in collected.items():
        if info[0] != 'pure':
            continue
        path = info[1] or ''
        if not path or not path.endswith('.py'):
            continue
        # 只扫描用户自己的本地文件；site-packages 里的第三方库文件即使
        # 落在项目目录内（如 venv 就建在项目里），其内部 import 也不算依赖
        if is_site_packages(path):
            continue
        if under(path, base_dir):
            local_files.add(os.path.normpath(path))
    local_files.add(os.path.normpath(entry))  # 入口必扫
    # 再加上项目目录下的全部 .py（含 PyInstaller 没收集到的 sys.path 注入目录）
    for p in iter_project_py(base_dir):
        local_files.add(os.path.normpath(p))

    imports_src = {}       # 顶层 import 名 -> set(来源 "相对路径:行号")
    full_names = {}        # 顶层 import 名 -> set(代码里实际写的完整模块名)
    optional_keys = set()  # 位于 try/except ImportError 里的来源（可选依赖）
    scanned = set()
    scanned_truncated = []

    # 单个项目最多解析这么多个本地文件。纯粹是防"误把 site-packages / 巨型目录当项目"
    # 的兜底，正常项目远达不到；一旦触顶会打日志，避免用户以为分析结果完整。
    max_scan_files = 2000

    def scan(path):
        if path in scanned:
            return
        if len(scanned) >= max_scan_files:
            if not scanned_truncated:
                scanned_truncated.append(True)
                print('[分析] 本地 .py 文件超过 %d 个，其余文件不再参与 import 扫描，'
                      '可能漏掉部分依赖' % max_scan_files, file=sys.stderr)
            return
        scanned.add(path)
        try:
            tree = ast.parse(read_source(path), path)
        except Exception:
            return
        try:
            rel = os.path.relpath(path, base_dir)
        except Exception:
            rel = os.path.basename(path)

        # 可选导入块：try: import x / except ImportError 之类。
        # 这类依赖缺失时程序会自动走降级分支，报成"缺失依赖"只会让用户
        # 去装一个用不上的包（uvloop / win32api / orjson 常用这种写法）。
        opt_ranges = []
        optional_nodes = []
        for n in ast.walk(tree):
            if isinstance(n, ast.Try) and n.handlers:
                catches = False
                for h in n.handlers:
                    t = h.type
                    names = []
                    if t is None:
                        catches = True
                    elif isinstance(t, ast.Name):
                        names = [t.id]
                    elif isinstance(t, ast.Tuple):
                        names = [e.id for e in t.elts if isinstance(e, ast.Name)]
                    if any(x in ('ImportError', 'ModuleNotFoundError', 'Exception', 'BaseException') for x in names):
                        catches = True
                if catches:
                    optional_nodes.append(n)
            elif isinstance(n, ast.If):
                # 类型检查块（if TYPE_CHECKING:）只在静态检查时成立，运行期不会执行；
                # if False: 同理是死代码。这两处的 import 都不是运行依赖。
                t = n.test
                is_static_only = (
                    (isinstance(t, ast.Name) and t.id == 'TYPE_CHECKING')
                    or (isinstance(t, ast.Attribute) and t.attr == 'TYPE_CHECKING')
                    or (isinstance(t, ast.Constant) and not t.value)
                )
                if is_static_only:
                    optional_nodes.append(n)
        for n in optional_nodes:
            start = n.lineno
            end = start
            for stmt in n.body:
                end = max(end, getattr(stmt, 'end_lineno', start) or start)
            opt_ranges.append((start, end))

        def record(mod, lineno):
            top = mod.split('.')[0]
            key = '%s:%d' % (rel, lineno)
            imports_src.setdefault(top, set()).add(key)
            # 记下完整模块名用于界面展示：import google.protobuf 显示成 "google"
            # 会让人误以为是另一个包，展示完整名才能对上代码
            full_names.setdefault(top, set()).add(mod)
            if any(a <= lineno <= b for a, b in opt_ranges):
                optional_keys.add(key)

        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for a in node.names:
                    record(a.name, node.lineno)
            elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
                # level>0 是相对导入（from . import x / from .m import y），
                # 那是包内部模块之间的互相引用，不是需要单独 pip 安装的依赖
                record(node.module, node.lineno)
            elif isinstance(node, ast.Call):
                # 动态导入启发式：importlib.import_module("x") / __import__("x")
                fn = node.func
                if isinstance(fn, ast.Attribute) and fn.attr == "import_module":
                    if node.args and isinstance(node.args[0], ast.Constant) and isinstance(node.args[0].value, str):
                        record(node.args[0].value, node.lineno)
                elif isinstance(fn, ast.Name) and fn.id == "__import__":
                    if node.args and isinstance(node.args[0], ast.Constant) and isinstance(node.args[0].value, str):
                        record(node.args[0].value, node.lineno)

    for f in sorted(local_files):
        scan(f)

    # ---------- 4. 包名映射 ----------
    # 注意：个别发行版的元数据缺失 Name（打包不规范），直接 d.metadata["Name"].lower()
    # 会抛 AttributeError 让整个分析崩掉，这里逐个兜底跳过。
    installed_dists = set()
    for d in importlib.metadata.distributions():
        try:
            name = (d.metadata.get('Name') or '').strip().lower()
        except Exception:
            continue
        if name:
            installed_dists.add(name)
    pkgs_map = {}
    try:
        pkgs_map = importlib.metadata.packages_distributions()
    except Exception:
        pass

    def to_pkg(mod, importable):
        """模块名 -> pip 包名；无法确定时返回 None（它不是一个可安装的依赖）。

        判定顺序：
          1. pipreqs 官方映射（cv2 -> opencv-python、win32api -> pywin32 等）
          2. 已装发行版声明的顶层模块（packages_distributions）
          3. 都查不到但模块确实能导入 → 它是某个包的内部模块/命名空间包/编译扩展
             （如 _yaml、win32ctypes），本身没有独立发行版。报成依赖只会让用户
             装进一个同名野包，必须排除。
          4. 都查不到且模块确实导入不了 → 真缺失，用模块名当包名作最佳猜测。
        """
        # ① 已装发行版声明的映射最权威：静态表会过期（pipreqs 把 MySQLdb 映射成
        #    只支持 Python 2 的 MySQL-python），而"环境里到底是哪个发行版提供了这个
        #    模块"是解释器自己说的，不会错。opencv-contrib-python 这类变体也能对上。
        dists = pkgs_map.get(mod) or []
        if dists:
            # 发行版名转小写，统一成 pip 可识别的规范名（如 "Requests" -> "requests"）
            return dists[0].lower()
        # ② 静态映射表（未安装时唯一可用的线索）
        pkg = MODULE_TO_PKG.get(mod.lower())
        if pkg:
            return pkg
        # ③ 能导入但没有发行版声明 → 内部模块/命名空间包/编译扩展，不是依赖
        if importable:
            return None
        # ④ 真缺失：用模块名当包名作最佳猜测
        return mod

    def req_version(dists, pkg):
        for d in dists:
            if norm(d) in constraints:
                return constraints[norm(d)]
        if norm(pkg) in constraints:
            return constraints[norm(pkg)]
        return ''

    def installed_ver(dists, pkg):
        try:
            for d in dists:
                if d.lower() in installed_dists:
                    return importlib.metadata.version(d)
            if pkg.lower() in installed_dists:
                return importlib.metadata.version(pkg)
        except Exception:
            pass
        return ''

    # ---------- 5. 差集判定 ----------
    stdlib = set(sys.stdlib_module_names)

    def has_module(imp):
        """权威兜底：直接问当前 Python 的 import 机制"这个模块能否被定位"。
        完全通用，不依赖 PyInstaller 的收集命名规则（纯模块名 / 二进制路径名 /
        其他任何形式），只要环境里真实存在（可 import），就判已装。"""
        try:
            return importlib.util.find_spec(imp) is not None
        except (ImportError, ValueError, AttributeError, ModuleNotFoundError):
            return False

    def hit_mod(imp):
        """PyInstaller 收集判定：环境里有没有该模块。
        收集结果含两类名字：
          - 纯模块名：cv2.core、numpy 等（a.pure）
          - 二进制路径名：cv2\\cv2.pyd、win32\\win32api.pyd（a.binaries）
        只查模块名会漏掉纯扩展模块（cv2 / win32api 这类），因此还要按目录前缀
        （cv2\\ 开头）和文件名（win32api.pyd）双向匹配。"""
        if imp in collected:
            return True
        if any(k.startswith(imp + '.') for k in collected):
            return True
        for k in collected:
            kp = k.replace('\\', '/')
            if kp.startswith(imp + '/'):
                return True
            if kp.rsplit('/', 1)[-1] in (imp + '.pyd', imp + '.dll', imp + '.so'):
                return True
        return False

    groups_missing = {}
    groups_resolved = {}
    for imp, srcs in imports_src.items():
        if imp in sys.builtin_module_names or is_stdlib(imp) or is_local_mod(imp):
            continue
        # 命中 Analysis 收集结果 或 环境可加载 → 已装；否则真缺失
        hit = hit_mod(imp) or has_module(imp)
        # 全部来源都在 try/except ImportError 里 → 可选依赖，缺失时程序自带降级，不报
        if not hit and srcs and all(k in optional_keys for k in srcs):
            continue
        pkg = to_pkg(imp, hit)
        if pkg is None:
            # 能导入、但没有任何发行版声明提供它 → 内部模块/命名空间包，不是依赖
            continue
        g = groups_missing if not hit else groups_resolved
        gk = norm(pkg)
        if gk not in g:
            g[gk] = {"pkg": pkg, "mods": set(), "srcs": set()}
        g[gk]["mods"].update(full_names.get(imp) or {imp})
        g[gk]["srcs"].update(srcs)

    def build_items(groups, installed_flag):
        items = []
        for gk in sorted(groups):
            g = groups[gk]
            pkg = g["pkg"]
            mods = sorted(g["mods"])
            dists = pkgs_map.get(mods[0], [])
            # 来源去重排序，最多展示 4 处（超出以「等 N 处」省略）
            srcs = sorted(g["srcs"])
            src_txt = ', '.join(srcs[:4]) + (' 等 %d 处' % len(srcs) if len(srcs) > 4 else '')
            items.append({
                "importName": ", ".join(mods),
                "pkgName": pkg,
                "version": req_version(dists, pkg),
                "installedVersion": installed_ver(dists, pkg),
                "installed": installed_flag,
                "source": src_txt,
            })
        return items

    missing = build_items(groups_missing, False)
    resolved = build_items(groups_resolved, True)

    # ---------- 6. 钩子补充（间接依赖：Analysis 收集到但代码未直接 import） ----------
    hook_groups = {}
    for name in pure_names:
        top = name.split('.')[0]
        info = collected[name]
        path = info[1] or ''
        if info[0] != 'pure':
            continue
        if top in stdlib or top in imports_src or top.startswith('_'):
            continue
        if name.startswith('pyimod') or name.startswith('pyi_rth') or top == 'PyInstaller':
            continue
        if is_local_mod(name) or not is_third_party(name, path):
            continue
        pkg = to_pkg(top, True)   # 钩子补充的都是已安装的模块
        if pkg is None or norm(pkg) in ('pyinstaller', 'pyarmor'):
            continue
        gk = norm(pkg)
        if gk not in hook_groups:
            hook_groups[gk] = {"pkg": pkg, "mods": set(), "srcs": set()}
        # 只记顶层模块名，避免 importName 拼接上百个子模块
        hook_groups[gk]["mods"].add(top)
    hook_extra = build_items(hook_groups, True)

    # ---------- 7. analysisModules：全部第三方模块全名（加密打包收集用） ----------
    # 只取纯 Python 模块（模块名形式，可安全作为 --hidden-import）；
    # 排除：标准库 / 本地模块 / PyInstaller 运行时 / 下划线开头的内部模块
    analysis_modules = []
    for name in pure_names:
        top = name.split('.')[0]
        info = collected[name]
        path = info[1] or ''
        if top in stdlib or top in sys.builtin_module_names or top.startswith('_'):
            continue
        if name.startswith('pyimod') or name.startswith('pyi_rth') or top == 'PyInstaller':
            continue
        if is_local_mod(name) or not is_third_party(name, path):
            continue
        analysis_modules.append(name)
    # 二进制扩展模块（cv2.pyd / win32api.pyd 等）：纯模块收集里没有它们，
    # 加密打包时 PyInstaller 看不到 import，必须 --hidden-import 激活对应钩子才能收集。
    # 取文件名的 .pyd 部分（去掉扩展名且为合法模块标识符），可安全作为 --hidden-import。
    for name, path, tc in a.binaries:
        base = name.replace('\\', '/').rsplit('/', 1)[-1]
        if base.endswith('.pyd'):
            mname = base[:-4]
            if mname and mname not in stdlib and re.match(r'^[A-Za-z_][A-Za-z0-9_]*$', mname):
                analysis_modules.append(mname)
    analysis_modules = sorted(set(analysis_modules))

    print(json.dumps({
        "missing": missing,
        "resolved": resolved,
        "hookExtra": hook_extra,
        "analysisModules": analysis_modules,
        "analysisSeconds": round(dt, 1),
        "isCached": is_cached,
    }, ensure_ascii=False))

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
