// Package portable 管理"内置便携版 Python"：
// 下载（国内镜像 + 多线程分块 + 断点续传）、解压、验证、列表、删除。
// 所有文件都放在 %LOCALAPPDATA%\dabao\runtime 下，不触碰系统 Python，
// 也不需要管理员权限。
package portable

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pypacker/internal/cmdutil"
	"pypacker/internal/paths"
)

// releaseTag python-build-standalone 的 release 标签（每次发版更新这里即可）
const releaseTag = "20260901"

// mirrors 国内可访问的 GitHub 加速代理（下载时逐个尝试，直到成功）
// 实测可用的国内代理（2026-09 验证：gh-proxy.com / ghproxy.net 支持 Range 206 可续传；
// homeboyc 已 403、moeyy 已失效，故只保留可用项，避免白白浪费探测时间）
var mirrors = []string{
	"https://gh-proxy.com/",
	"https://ghproxy.net/",
}

// VersionInfo 一个可下载的便携版版本（供前端展示与选择）
type VersionInfo struct {
	Version string `json:"version"` // 完整版本号，如 3.12.14
	Label   string `json:"label"`   // 展示名，如 3.12
	SizeMB  int64  `json:"sizeMB"`  // 压缩包大小（MB）
	Default bool   `json:"default"` // 是否默认推荐
}

// Versions 可下载的便携版版本清单
func Versions() []VersionInfo {
	return []VersionInfo{
		{Version: "3.10.21", Label: "3.10", SizeMB: 20},
		{Version: "3.11.16", Label: "3.11", SizeMB: 24},
		{Version: "3.12.14", Label: "3.12", SizeMB: 20, Default: true},
		{Version: "3.13.15", Label: "3.13", SizeMB: 20},
		{Version: "3.14.7", Label: "3.14", SizeMB: 21},
	}
}

// versionOK 判断版本号是否在可下载清单中
func versionOK(version string) bool {
	for _, v := range Versions() {
		if v.Version == version {
			return true
		}
	}
	return false
}

// urlOf 生成某版本在指定镜像前缀下的下载地址
func urlOf(mirror, version string) string {
	raw := "https://github.com/astral-sh/python-build-standalone/releases/download/" +
		releaseTag + "/cpython-" + version + "+" + releaseTag +
		"-x86_64-pc-windows-msvc-install_only_stripped.tar.gz"
	return mirror + raw
}

// RootDir 便携版根目录：C:\Users\用户名\AppData\Local\dabao\runtime
func RootDir() string {
	return paths.RuntimeRoot()
}

// PortablePython 一个已下载的便携版信息
type PortablePython struct {
	Version string `json:"version"` // 版本号（目录名 cpython-<版本>）
	Path    string `json:"path"`    // python.exe 完整路径
	Dir     string `json:"dir"`     // 版本文件夹
	SizeMB  int64  `json:"sizeMB"`  // 文件夹占用大小（MB）
}

// List 列出所有已下载的便携版
func List() []PortablePython {
	entries, err := os.ReadDir(RootDir())
	if err != nil {
		return nil
	}
	var out []PortablePython
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "cpython-") {
			continue
		}
		dir := filepath.Join(RootDir(), name)
		py := filepath.Join(dir, "python", "python.exe")
		if _, err := os.Stat(py); err != nil {
			continue
		}
		out = append(out, PortablePython{
			Version: strings.TrimPrefix(name, "cpython-"),
			Path:    py,
			Dir:     dir,
			SizeMB:  dirSizeMB(dir),
		})
	}
	return out
}

// dirSizeMB 计算文件夹占用大小（MB，向上取整）
func dirSizeMB(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return (total + 1024*1024 - 1) / (1024 * 1024)
}

// Downloader 下载过程的回调与取消控制
type Downloader struct {
	OnLog      func(string)            // 阶段日志（中文）
	OnDownload func(done, total int64) // 下载进度（字节），供界面算百分比和速度
	Ctx        context.Context         // 取消信号：点击「取消」时取消该上下文，所有在途连接立即断开
}

// cleanupDownload 删除下载残留的残缺文件（取消或失败时调用，不留垃圾）
func cleanupDownload(dest string) {
	_ = os.Remove(dest)
	for i := int64(0); i < chunkCount; i++ {
		_ = os.Remove(fmt.Sprintf("%s.part%d", dest, i))
	}
}

// Download 下载指定版本的便携版 Python 并解压验证。
// 返回 python.exe 的完整路径；重复下载时若已存在则直接返回。
func Download(version string, d *Downloader) (string, error) {
	ctx := dlCtx(d)
	if !versionOK(version) {
		return "", fmt.Errorf("不支持的版本号：%s", version)
	}
	root := RootDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("无法创建便携版目录 %s：%v\n建议：确认 %s 是否可写", root, err, root)
	}

	// 已下载过：直接返回
	existDir := filepath.Join(root, "cpython-"+version)
	existPy := filepath.Join(existDir, "python", "python.exe")
	if _, err := os.Stat(existPy); err == nil {
		if d != nil && d.OnLog != nil {
			d.OnLog("该版本已下载，直接使用")
		}
		return existPy, nil
	}

	tarPath := filepath.Join(root, "cpython-"+version+".tar.gz")
	log := func(s string) {
		if d != nil && d.OnLog != nil {
			d.OnLog(s)
		}
	}

	// ---- 1. 并发探测可用镜像 → 下载（取消时立即中断并清理残缺文件）----
	// 说明：串行逐个探测时，每个慢镜像最多拖 30 秒（两个坏镜像就是 1 分钟），
	// 用户会看到进度一直停在 0。这里改为所有镜像并发探测（10 秒内必有结果），
	// 谁先响应就用谁，下载速度不受影响。
	log("1、正在连接下载代理…")
	var lastErr error
	// 跨代理续传：记录第一次探测到的文件大小，换代理时若大小不一致（内容不同）才清空重下
	var keepTotal int64 = -1
	// 按优先级逐个尝试（gh-proxy.com 在前，实测下载更稳；ghproxy.net 作备用）。
	// 只有两个代理，不再并发探测，直接顺序探测 + 下载，失败才换下一个（分块保留续传）。
	for _, mirror := range mirrors {
		if cancelled(d) {
			cleanupDownload(tarPath)
			return "", fmt.Errorf("下载已取消")
		}
		// 探测当前代理：确认支持断点续传并拿到文件总大小（10 秒内必出结果）
		pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
		total, ok, perr := probeSize(pctx, dlClient, urlOf(mirror, version))
		pcancel()
		if perr != nil || !ok || total <= 0 {
			lastErr = fmt.Errorf("%s 不可用：%v", mirror, perr)
			log(fmt.Sprintf("%s 代理不可用，尝试下一个…", mirror))
			continue
		}
		if keepTotal > 0 && total != keepTotal {
			cleanupDownload(tarPath)
			log("代理返回的文件大小不一致，已重新开始下载…")
		}
		keepTotal = total
		log(fmt.Sprintf("2、已连接 %s 代理", mirror))
		// 真正下载前先告知：代理去 GitHub 取数需要时间，进度会暂时停在 0
		log("3、等待代理拉取镜像中，预计30秒…")
		if err := downloadFile(ctx, urlOf(mirror, version), tarPath, total, d); err != nil {
			if cancelled(d) {
				cleanupDownload(tarPath)
				return "", fmt.Errorf("下载已取消")
			}
			lastErr = err
			// 失败不删已下好的分块：切换代理继续下载剩余部分（不同镜像内容相同，Range 请求通用）
			log("该镜像下载失败：" + err.Error() + "，切换代理继续下载…")
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return "", fmt.Errorf("所有镜像均无法下载，请检查网络后重试：%v", lastErr)
	}
	if cancelled(d) {
		cleanupDownload(tarPath)
		return "", fmt.Errorf("下载已取消")
	}

	// ---- 2. 解压到临时目录后改名（避免解压一半留下残缺目录）----
	log("5、下载完成，正在合并解压…")
	tmpDir := existDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := extractTarGz(tarPath, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("解压失败：%v\n建议：删除 %s 后重新下载", err, tarPath)
	}
	if err := os.RemoveAll(existDir); err != nil {
		return "", fmt.Errorf("清理旧版本目录失败：%v", err)
	}
	if err := os.Rename(tmpDir, existDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("移动目录失败：%v", err)
	}
	_ = os.Remove(tarPath) // 压缩包用完即删，节省空间

	// ---- 3. 验证 python.exe 可用 ----
	log("正在验证便携版 Python…")
	if _, err := os.Stat(existPy); err != nil {
		return "", fmt.Errorf("解压后未找到 python.exe，下载可能不完整")
	}
	if err := verifyPython(existPy, version); err != nil {
		return "", err
	}
	return existPy, nil
}

// Delete 删除指定版本的整个文件夹（含已装依赖）
func Delete(version string) error {
	dir := filepath.Join(RootDir(), "cpython-"+version)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("未找到该便携版 Python（%s）", version)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除失败：文件可能正被占用（请先关闭正在使用该环境的打包/安装任务）")
	}
	return nil
}

// ---- 下载实现（多线程分块 + 断点续传，context 统一控制取消）----

const chunkCount = 4 // 并发分块数：4 线程对免费代理更友好（8 线程曾导致部分分块断流、整批作废）

// dlTransport 下载专用 HTTP 客户端：连接/响应头都设了短超时。
// 没有超时的话，镜像连接卡住时进度会长时间停在 0%（用户看到"卡死"）。
// 连接 10 秒内建不起来、响应头 15 秒内收不到，直接失败换下一个镜像。
var dlTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 15 * time.Second,
}

// dlClient 下载/探测共用客户端（http.Client 可并发安全使用）
var dlClient = &http.Client{Transport: dlTransport}

// cancelled 是否已收到取消信号（立即返回，供各阶段检查）
func cancelled(d *Downloader) bool {
	return d != nil && d.Ctx != nil && d.Ctx.Err() != nil
}

// dlCtx 取取消上下文（未设置时用后台上下文）
func dlCtx(d *Downloader) context.Context {
	if d != nil && d.Ctx != nil {
		return d.Ctx
	}
	return context.Background()
}

// downloadFile 按已知总大小多线程 Range 分块下载到 dest（支持断点续传 + 失败块重试）。
// total 由上层 probeMirrors 探测得到，这里不再重复探测（避免连接慢的镜像拖住进度）。
// 某块失败不会推倒重来：已下好的块保留，最多重试 3 轮只补未完成的块。
func downloadFile(ctx context.Context, url, dest string, total int64, d *Downloader) error {
	// 已完整下载：直接完成
	if fi, err := os.Stat(dest); err == nil && fi.Size() == total {
		return nil
	}

	// ---- 分块下载：每块独立 .part 文件；已完整块跳过（断点续传）----
	// 整体超时 10 分钟，防止某个块连接卡死拖住整个任务
	chunkCtx, chunkCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer chunkCancel()
	chunkSize := chunkSizeCalc(total)
	var wg sync.WaitGroup
	var done atomic.Int64
	if d != nil && d.OnDownload != nil {
		d.OnDownload(0, total)
	}
	// 首次有数据到达时，把"等待代理拉取"切换为"多进程下载中"（只提示一次）
	var dataOnce sync.Once
	notify := func(done, total int64) {
		dataOnce.Do(func() {
			if d != nil && d.OnLog != nil {
				d.OnLog("4、多进程下载中，预计30秒…")
			}
		})
		if d != nil && d.OnDownload != nil {
			d.OnDownload(done, total)
		}
	}
	// 失败块重试：最多 3 轮，每轮只下载未完成的块；全部轮次结束后仍失败才整体报错
	const maxAttempts = 3
	var firstErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		firstErr = nil
		var mu sync.Mutex
		beforeDone := done.Load()
		for i := int64(0); i < chunkCount; i++ {
			start := i * chunkSize
			end := start + chunkSize - 1
			if start >= total {
				break
			}
			if end >= total {
				end = total - 1
			}
			part := fmt.Sprintf("%s.part%d", dest, i)
			// 已完整下好的块直接跳过（续传/重试都靠这个）
			if fi, err := os.Stat(part); err == nil && fi.Size() == end-start+1 {
				done.Add(end - start + 1)
				continue
			}
			wg.Add(1)
			go func(start, end, i int64) {
				defer wg.Done()
				// 每块独立 90 秒超时：单块连接卡住（镜像排队/断流）不会拖死整批，超时后只重试该块。
				// 20MB 分 4 块，每块约 5MB，正常网络几十秒足够，90 秒非常宽松。
				bctx, bcancel := context.WithTimeout(chunkCtx, 90*time.Second)
				defer bcancel()
				if err := downloadChunk(bctx, url, fmt.Sprintf("%s.part%d", dest, i), start, end, d); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				if fi, err := os.Stat(fmt.Sprintf("%s.part%d", dest, i)); err == nil {
					done.Add(fi.Size())
					notify(done.Load(), total)
				}
			}(start, end, i)
		}
		wg.Wait()
		if firstErr == nil {
			break
		}
		// 这一轮一个字节都没下到（进度没动）：说明该代理去 GitHub 取数排队/断流，
		// 同镜像重试也是白等，直接返回错误让上层换代理。
		if done.Load() == beforeDone {
			break
		}
		// 有进度但没下完：不推倒，稍作停顿后进入下一轮只补剩余块
		if attempt < maxAttempts-1 {
			if d != nil && d.OnLog != nil {
				d.OnLog("部分分块连接中断，正在自动重试剩余分块…")
			}
			select {
			case <-chunkCtx.Done():
			case <-time.After(3 * time.Second):
			}
			if chunkCtx.Err() != nil {
				break
			}
		}
	}
	if firstErr != nil {
		return fmt.Errorf("分块下载多次失败：%v", firstErr)
	}

	// ---- 3. 合并分块 ----
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	for i := int64(0); i < chunkCount; i++ {
		part := fmt.Sprintf("%s.part%d", dest, i)
		f, err := os.Open(part)
		if err != nil {
			continue
		}
		if _, err := io.Copy(out, f); err != nil {
			f.Close()
			out.Close()
			return err
		}
		f.Close()
		_ = os.Remove(part)
	}
	out.Close()
	if d != nil && d.OnDownload != nil {
		d.OnDownload(total, total)
	}
	return nil
}

// probeSize 请求第 1 个字节：返回文件总大小、是否支持 Range。
// 支持 Range（206）时从 Content-Range 解析总大小；返回 200 表示镜像不支持断点续传。
func probeSize(ctx context.Context, client *http.Client, url string) (total int64, supportRange bool, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 206:
		cr := resp.Header.Get("Content-Range")
		if idx := strings.LastIndex(cr, "/"); idx >= 0 {
			if _, err := fmt.Sscanf(cr[idx+1:], "%d", &total); err == nil && total > 0 {
				return total, true, nil
			}
		}
		return 0, false, fmt.Errorf("镜像响应异常，无法解析文件大小")
	case resp.StatusCode == 200:
		if resp.ContentLength > 0 {
			return resp.ContentLength, false, nil
		}
		return 0, false, fmt.Errorf("镜像不支持断点续传且无法获取文件大小")
	default:
		return 0, false, fmt.Errorf("镜像返回异常状态码 %d", resp.StatusCode)
	}
}

// chunkSizeCalc 计算分块大小（避免整除为 0）
func chunkSizeCalc(total int64) int64 {
	if total < chunkCount {
		return 1
	}
	return total / chunkCount
}

// downloadChunk 下载 [start, end] 字节范围到 part 文件（支持续传，可随时取消）
func downloadChunk(ctx context.Context, url, part string, start, end int64, d *Downloader) error {
	// 已存在部分数据：从断点继续
	var resume int64
	if fi, err := os.Stat(part); err == nil {
		resume = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start+resume, end))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := dlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 206 {
		return fmt.Errorf("镜像不支持断点续传（状态码 %d）", resp.StatusCode)
	}

	// 206：从 resume 偏移继续写
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(resume, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("已取消")
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

// ---- 解压 ----

// extractTarGz 解压 tar.gz 到 dest（自动创建目录，防止路径穿越）
func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	destClean := filepath.Clean(dest)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Join(destClean, filepath.FromSlash(hdr.Name))
		if name != destClean && !strings.HasPrefix(name, destClean+string(os.PathSeparator)) {
			return fmt.Errorf("压缩包内含非法路径：%s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return err
			}
			out, err := os.Create(name)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// verifyPython 运行 python.exe --version 确认可用且版本匹配
func verifyPython(exe, version string) error {
	cmd := cmdutil.Command(exe, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("便携版 Python 验证失败：%v\n建议：删除后重新下载", err)
	}
	got := strings.TrimSpace(string(out))
	short := version[:strings.LastIndex(version, ".")] // 3.12
	if !strings.Contains(got, short) {
		return fmt.Errorf("便携版 Python 版本异常（期望 %s，实际 %s）", version, got)
	}
	return nil
}
