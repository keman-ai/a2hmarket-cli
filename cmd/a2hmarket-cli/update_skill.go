package main

// update_skill.go — 更新 OpenClaw workspace 中的 a2hmarket skill。

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/urfave/cli/v2"
)

const (
	skillPackageURL = "https://a2hmarket.ai/github/keman-ai/a2hmarket/releases/latest/download/skill-package.zip"
	skillName       = "a2hmarket"
)

func updateSkillCommand() *cli.Command {
	return &cli.Command{
		Name:   "update-skill",
		Usage:  "Update the a2hmarket skill in OpenClaw workspace",
		Action: updateSkillCmd,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "Install even if skill directory does not exist yet"},
		},
	}
}

func updateSkillCmd(c *cli.Context) error {
	force := c.Bool("force")

	openclawDir := findOpenclawStateDir()
	skillDir := filepath.Join(openclawDir, "workspace", "skills", skillName)

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		if !force {
			fmt.Printf("Skill directory not found: %s\n", skillDir)
			fmt.Println("Run with --force to install for the first time.")
			return nil
		}
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			return fmt.Errorf("create skill directory: %w", err)
		}
		common.Infof("Created skill directory: %s", skillDir)
	}

	// Read current version before update.
	oldVersion := readSkillVersion(skillDir)

	fmt.Println("Downloading latest skill package...")
	common.Infof("Downloading from %s", skillPackageURL)

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(skillPackageURL)
	if err != nil {
		return fmt.Errorf("download skill package: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download skill package: HTTP %d", resp.StatusCode)
	}

	// Save to temp file.
	tmpFile, err := os.CreateTemp("", "a2hmarket-skill-*.zip")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download skill package: %w", err)
	}
	tmpFile.Close()

	// Extract zip into skill directory, replacing existing files.
	if err := extractSkillZip(tmpPath, skillDir); err != nil {
		return fmt.Errorf("extract skill package: %w", err)
	}

	newVersion := readSkillVersion(skillDir)
	if oldVersion != "" && newVersion != "" && oldVersion != newVersion {
		fmt.Printf("✓ Skill updated: %s → %s\n", oldVersion, newVersion)
	} else if newVersion != "" {
		fmt.Printf("✓ Skill installed: %s (version %s)\n", skillName, newVersion)
	} else {
		fmt.Printf("✓ Skill updated: %s\n", skillName)
	}
	fmt.Printf("  Location: %s\n", skillDir)
	return nil
}

// findOpenclawStateDir returns the OpenClaw state directory.
func findOpenclawStateDir() string {
	if v := os.Getenv("OPENCLAW_STATE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("OPENCLAW_HOME"); v != "" {
		return filepath.Join(v, ".openclaw")
	}
	return filepath.Join(os.Getenv("HOME"), ".openclaw")
}

// readSkillVersion reads the version from SKILL.md frontmatter.
func readSkillVersion(skillDir string) string {
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return ""
	}
	content := string(data)
	// Parse YAML frontmatter between --- delimiters.
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		}
	}
	return ""
}

// extractSkillZip extracts a skill zip package into destDir.
// The zip may contain files at the root or inside a single top-level directory.
// We detect and strip the common prefix so files land directly in destDir.
func extractSkillZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	// Detect common prefix (e.g. "a2hmarket/" inside the zip).
	prefix := detectCommonPrefix(r.File)

	for _, f := range r.File {
		// Strip common prefix.
		name := f.Name
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destDir, name)

		// Prevent zip slip.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(target), 0755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// detectCommonPrefix returns the single top-level directory prefix if all
// files share one (e.g. "a2hmarket/"), or "" if files are at the root.
func detectCommonPrefix(files []*zip.File) string {
	if len(files) == 0 {
		return ""
	}
	var prefix string
	for _, f := range files {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			// File at root level — no common prefix.
			return ""
		}
		dir := parts[0] + "/"
		if prefix == "" {
			prefix = dir
		} else if prefix != dir {
			return ""
		}
	}
	return prefix
}
