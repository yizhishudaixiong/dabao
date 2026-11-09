package deps

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"pypacker/internal/config"
)

// 复现核心工作流：分析 -> 报缺失 -> 装包 -> 再分析。
// PyInstaller 的 Analysis 缓存只看「上次收集到的文件」的 mtime，
// 新装的包不在上次的 pure 清单里，不会让缓存失效。
func TestInstallThenReanalyze(t *testing.T) {
	if os.Getenv("DABAO_E2E") == "" {
		t.Skip("端到端测试默认跳过（设置 DABAO_E2E=1 开启；需联网建 venv 装包）")
	}
	py := `C:/Users/xiong/AppData/Local/Programs/Python/Python312/python.exe`
	if _, err := os.Stat(py); err != nil {
		t.Skip("本机没有用于测试的 Python")
	}
	dir := t.TempDir()
	venv := filepath.Join(dir, "venv")
	if out, err := exec.Command(py, "-m", "venv", venv).CombinedOutput(); err != nil {
		t.Skipf("创建 venv 失败: %v %s", err, out)
	}
	vpy := filepath.Join(venv, "Scripts", "python.exe")
	if _, err := os.Stat(vpy); err != nil {
		t.Skip("venv 结构异常")
	}
	entry := filepath.Join(dir, "main.py")
	if err := os.WriteFile(entry, []byte("import requests\nprint(requests.__version__)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 真实流程里 App.AnalyzeDeps 会先自动补装 PyInstaller
	pi := exec.Command(vpy, "-m", "pip", "install", "-q", "pyinstaller",
		"-i", "https://mirrors.aliyun.com/pypi/simple/", "--disable-pip-version-check")
	if out, err := pi.CombinedOutput(); err != nil {
		t.Skipf("venv 内安装 pyinstaller 失败: %v %s", err, out)
	}

	r1, err := Analyze(entry, vpy, nil)
	if err != nil {
		t.Fatalf("第一次分析失败: %v", err)
	}
	found1 := false
	for _, m := range r1.Missing {
		if m.PkgName == "requests" {
			found1 = true
		}
	}
	t.Logf("首次分析: 缺失 %d 项, requests 缺失=%v, 耗时 %.1fs, cached=%v",
		len(r1.Missing), found1, r1.AnalysisSeconds, r1.IsCached)

	// 装包（用国内镜像加速）
	inst := exec.Command(vpy, "-m", "pip", "install", "-q", "requests",
		"-i", "https://mirrors.aliyun.com/pypi/simple/", "--disable-pip-version-check")
	if out, err := inst.CombinedOutput(); err != nil {
		t.Skipf("venv 内安装 requests 失败（跳过）: %v %s", err, out)
	}

	r2, err := Analyze(entry, vpy, nil)
	if err != nil {
		t.Fatalf("第二次分析失败: %v", err)
	}
	found2 := false
	for _, m := range r2.Missing {
		if m.PkgName == "requests" {
			found2 = true
		}
	}
	t.Logf("装包后分析: 缺失 %d 项, requests 缺失=%v, 耗时 %.1fs, cached=%v",
		len(r2.Missing), found2, r2.AnalysisSeconds, r2.IsCached)

	// 加密打包靠这两份清单补 --collect-all / --hidden-import
	in := func(list []config.DepInfo, pkg string) bool {
		for _, d := range list {
			if d.PkgName == pkg {
				return true
			}
		}
		return false
	}
	t.Logf("装包后 resolved 含 requests=%v, hookExtra 含 requests=%v",
		in(r2.Resolved, "requests"), in(r2.HookExtra, "requests"))
	hit := false
	for _, m := range r2.AnalysisModules {
		if m == "requests" || len(m) > 9 && m[:9] == "requests." {
			hit = true
			break
		}
	}
	t.Logf("装包后 analysisModules 含 requests=%v（共 %d 项）", hit, len(r2.AnalysisModules))
	if !hit {
		t.Errorf("【BUG】装包后 analysisModules 仍不含 requests：加密打包不会 --collect-all requests，产物运行时报 ModuleNotFoundError")
	}
	if found2 {
		t.Fatalf("【BUG 复现】装完 requests 后重新分析仍报缺失：分析缓存未随环境变化失效")
	}
}
