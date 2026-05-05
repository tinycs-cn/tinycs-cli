package commands

import (
	"flag"
	"fmt"

	"github.com/bootcraft-cn/cli/internal/client"
	"github.com/bootcraft-cn/cli/internal/config"
)

// StagesCommand implements "bootcraft stages".
// Reads the current project's course slug (via resolveProject) then calls
// GET /v1/courses/{slug} to list all stage slugs with their positions and names.
func StagesCommand(args []string) error {
	flags := flag.NewFlagSet("stages", flag.ContinueOnError)
	apiURL := flags.String("api-url", "", "API 地址（内部测试用）")
	if err := flags.Parse(args); err != nil {
		return err
	}

	course, _, _, err := resolveProject()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	baseURL := cfg.GetAPIURL(*apiURL)
	c := client.New(baseURL, cfg.GetToken())

	detail, err := c.GetCourse(course)
	if err != nil {
		return fmt.Errorf("获取课程信息失败: %w", err)
	}

	fmt.Printf("课程: %s  (模式: %s)\n\n", detail.Slug, detail.ProgressionMode)
	fmt.Printf("%-4s  %-28s %s\n", "序号", "Slug", "名称")
	fmt.Printf("%-4s  %-28s %s\n", "----", "----------------------------", "----")
	for _, s := range detail.Stages {
		fmt.Printf("%-4d  %-28s %s\n", s.Position, s.Slug, s.Name)
	}
	return nil
}
