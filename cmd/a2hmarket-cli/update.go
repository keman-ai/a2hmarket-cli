package main

// update.go — 自检新版本、自动更新。

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/urfave/cli/v2"
)

const (
	repoOwner = "keman-ai"
	repoName  = "a2hmarket-cli"
	// 自建代理（国内加速），与 install.sh 一致
	a2hProxy = "https://a2hmarket.ai/github"
)

// latestRelease 从 GitHub API 获取最新 release 信息
type latestRelease struct {
	TagName string `json:"tag_name"`
}

func updateCommand() *cli.Command {
	return &cli.Command{
		Name:   "update",
		Usage:  "Check for new version and auto-update to latest release",
		Action: updateCmd,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "check-only", Usage: "Only check if update available, do not install"},
		},
	}
}

func updateCmd(c *cli.Context) error {
	checkOnly := c.Bool("check-only")

	// 获取最新 release tag
	latestTag, err := fetchLatestTag()
	if err != nil {
		return fmt.Errorf("获取最新版本失败: %w", err)
	}

	current := normalizeVersion(version)
	latest := normalizeVersion(latestTag)

	if !isNewer(latest, current) {
		common.Infof("当前已是最新版本: %s", version)
		fmt.Printf("Current: %s (latest)\n", version)
		return nil
	}

	common.Infof("发现新版本: %s (当前: %s)", latestTag, version)
	fmt.Printf("New version available: %s (current: %s)\n", latestTag, version)

	if checkOnly {
		fmt.Println("Run 'a2hmarket-cli update' (without --check-only) to install.")
		return nil
	}

	// 下载并替换
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取可执行文件路径: %w", err)
	}

	if err := downloadAndReplace(binPath, latestTag); err != nil {
		return fmt.Errorf("更新失败: %w", err)
	}

	fmt.Printf("✓ Updated to %s\n", latestTag)
	common.Infof("Update completed: %s", latestTag)
	return nil
}

func fetchLatestTag() (string, error) {
	urls := []string{
		fmt.Sprintf("%s/repos/%s/%s/releases/latest", a2hProxy, repoOwner, repoName),
		fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName),
	}

	client := &http.Client{Timeout: 15}
	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		var rel latestRelease
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		if rel.TagName != "" {
			return rel.TagName, nil
		}
	}
	return "", fmt.Errorf("无法从 GitHub 获取最新 release")
}

func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "v") {
		s = s[1:]
	}
	return s
}

// isNewer 比较版本，a > b 返回 true
func isNewer(a, b string) bool {
	pa := parseVersion(a)
	pb := parseVersion(b)
	for i := 0; i < 3; i++ {
		va := 0
		vb := 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va > vb {
			return true
		}
		if va < vb {
			return false
		}
	}
	return false
}

func parseVersion(s string) []int {
	var parts []int
	for _, p := range strings.Split(s, ".") {
		var n int
		fmt.Sscanf(strings.TrimSpace(p), "%d", &n)
		parts = append(parts, n)
	}
	return parts
}

func downloadAndReplace(binPath, tag string) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	ext := "tar.gz"
	if osName == "windows" {
		ext = "zip"
	}
	filename := fmt.Sprintf("a2hmarket-cli_%s_%s.%s", osName, arch, ext)

	baseURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, filename)
	urls := []string{
		fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", a2hProxy, repoOwner, repoName, tag, filename),
		baseURL,
	}

	var body io.ReadCloser
	for _, u := range urls {
		resp, err := http.Get(u)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body = resp.Body
		break
	}
	if body == nil {
		return fmt.Errorf("下载失败，请检查网络或访问 https://github.com/%s/%s/releases", repoOwner, repoName)
	}
	defer body.Close()

	tmpDir, err := os.MkdirTemp("", "a2hmarket-cli-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, filename)
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// 解压
	if ext == "tar.gz" {
		if err := extractTarGz(archivePath, tmpDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("zip 格式暂不支持，请手动运行 install.sh 更新")
	}

	newBinPath, err := findBinaryInDir(tmpDir, "a2hmarket-cli")
	if err != nil {
		return err
	}

	// 替换：先写到临时位置，再 rename（原子替换）
	tmpBin := binPath + ".new"
	if err := copyFile(newBinPath, tmpBin); err != nil {
		return err
	}
	if err := os.Chmod(tmpBin, 0755); err != nil {
		os.Remove(tmpBin)
		return err
	}
	if err := os.Rename(tmpBin, binPath); err != nil {
		os.Remove(tmpBin)
		return fmt.Errorf("替换失败（可能无写权限）: %w", err)
	}

	return nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
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
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		path := filepath.Join(destDir, h.Name)
		if h.Typeflag == tar.TypeDir {
			os.MkdirAll(path, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0755)
		out, err := os.Create(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	return nil
}

func findBinaryInDir(dir, name string) (string, error) {
	var found string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info != nil && !info.IsDir() && filepath.Base(path) == name {
			found = path
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("解压后未找到二进制文件 %s", name)
	}
	return found, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
