// Local-only acceptance harness. It never reads production configuration or data.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/api"
	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/Nyakooo/grok-bot-dock/internal/webui"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	dir, err := os.MkdirTemp("", "bolt-topic-acceptance-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	db, err := store.Open(filepath.Join(dir, "acceptance.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	names := []string{"产品迭代", "Agent 工作流观察", "Tibo 重置监控", "家庭网络升级", "开源项目跟踪", "个人研究笔记"}
	groups := []string{"工程与产品", "AI 与技术", "生活与消费", "个人研究"}
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("验收主题 %02d", i+1)
		if i < len(names) {
			name = "验收：" + names[i]
		}
		source := "grok"
		if i%2 == 0 {
			source = "codex"
		}
		input := store.ReportInput{Source: source, BotID: fmt.Sprintf("acceptance-task-%02d", i), BotName: "隔离验收任务", TopicID: fmt.Sprintf("acceptance-topic-%02d", i), TopicName: name, ExecutedAt: time.Now().Add(-time.Duration(i) * time.Hour), Title: name + "的正式报告", Summary: "隔离环境合成数据，用于验证主题整理、检索、阅读和分享。", Content: "# " + name + "\n\n## 验收结论\n\n这是一份本地隔离验收报告。\n\n## 原始证据\n\n中文全文检索标记：跨主题证据。\n\n| 项目 | 结果 |\n| --- | --- |\n| 本地环境 | 合成样例 |\n\n```go\nfmt.Println(\"acceptance\")\n```", ExternalID: "initial-execution"}
		report, _, err := db.CreateReport(ctx, input)
		if err != nil {
			return err
		}
		followed, pinned := i < 6, i < 3
		group := groups[i%len(groups)]
		if err = db.PatchTopic(ctx, report.TopicID, store.TopicPatch{Group: &group, Followed: &followed, Pinned: &pinned}); err != nil {
			return err
		}
	}
	handler := api.New(db, events.New(), "local-topic-test-only", 1<<20, webui.Files).Handler()
	server := &http.Server{Addr: "127.0.0.1:18876", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("Isolated topic acceptance server: http://127.0.0.1:18876 (synthetic data only)")
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
