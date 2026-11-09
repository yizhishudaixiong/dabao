package portable

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestChunkRangesCoverFile 分块必须无缝覆盖整个文件：
// 曾经用 total/chunkCount 向下取整，文件大小不能被 4 整除时末尾几个字节没人下载，
// 合并出的压缩包短一截，直到解压才报错（且错误信息与下载毫无关系）。
func TestChunkRangesCoverFile(t *testing.T) {
	totals := []int64{1, 2, 3, 4, 5, 7, 100, 101, 102, 103, 21980728, 21980729, 21980731}
	for _, total := range totals {
		size := chunkSizeCalc(total)
		var covered, expectStart int64
		for i := int64(0); i < chunkCount; i++ {
			start, end, ok := chunkRange(i, total, size)
			if !ok {
				break
			}
			if start != expectStart {
				t.Fatalf("total=%d 第 %d 块起点 %d，期望 %d（分块出现空洞或重叠）", total, i, start, expectStart)
			}
			if end < start || end >= total {
				t.Fatalf("total=%d 第 %d 块区间非法: [%d,%d]", total, i, start, end)
			}
			covered += end - start + 1
			expectStart = end + 1
		}
		if covered != total {
			t.Fatalf("total=%d 只覆盖了 %d 字节（差 %d 字节）", total, covered, total-covered)
		}
	}
}

// TestChunkSizeNeverZero 分块大小不能为 0，否则 Range 请求非法
func TestChunkSizeNeverZero(t *testing.T) {
	for _, total := range []int64{0, 1, 2, 3, 4, 5} {
		if got := chunkSizeCalc(total); got < 1 {
			t.Fatalf("total=%d 时 chunkSizeCalc 返回 %d", total, got)
		}
	}
}

// TestDownloadFileNonDivisible 端到端验证分块下载：
// 用 1003 字节（1003 % 4 = 3，旧实现会丢最后 3 字节）起一个支持 Range 的本地服务，
// 下载后必须与源数据逐字节一致。这条测试就是"向下取整丢尾部"那个 bug 的回归点。
func TestDownloadFileNonDivisible(t *testing.T) {
	data := make([]byte, 1003)
	for i := range data {
		data[i] = byte(i % 251)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x.tar.gz", time.Unix(0, 0), bytes.NewReader(data))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.tar.gz")
	if err := downloadFile(context.Background(), srv.URL, dest, int64(len(data)), nil); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("下载内容与源不一致：期望 %d 字节，实际 %d 字节", len(data), len(got))
	}
}

// TestDownloadFileResume 断点续传：已有部分分块时只补剩余字节，最终内容仍完整
func TestDownloadFileResume(t *testing.T) {
	data := make([]byte, 5000)
	for i := range data {
		data[i] = byte(i % 253)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x.tar.gz", time.Unix(0, 0), bytes.NewReader(data))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.tar.gz")
	// 预置第 0 块的前 300 字节，模拟上次下载中断
	if err := os.WriteFile(partPath(dest, 0), data[:300], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadFile(context.Background(), srv.URL, dest, int64(len(data)), nil); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("续传后内容不一致：期望 %d 字节，实际 %d 字节", len(data), len(got))
	}
}

// Delete 会 RemoveAll，必须先挡住路径逃逸（版本号来自目录名，仍不可信任）
func TestDeleteRejectsUnsafeVersion(t *testing.T) {
	for _, v := range []string{"", "..", "../..", `..\..`, "3.12/../../x", "3.12.14 ", "v3.12", "3.12.14;rm"} {
		if err := Delete(v); err == nil {
			t.Errorf("应拒绝版本号 %q", v)
		}
	}
}
