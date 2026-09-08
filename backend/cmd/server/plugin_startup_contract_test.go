package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainStartsPluginManagerBeforePromptAudit(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	pluginStart := strings.Index(text, "app.PluginManager.Start(context.Background())")
	promptAuditStart := strings.Index(text, "app.PromptAudit.Start(context.Background())")
	if pluginStart < 0 {
		t.Fatal("runMainServer must start app.PluginManager")
	}
	if promptAuditStart < 0 {
		t.Fatal("runMainServer must keep starting app.PromptAudit")
	}
	if pluginStart > promptAuditStart {
		t.Fatal("plugin manager must start before prompt audit")
	}
	if !strings.Contains(text[pluginStart:promptAuditStart], "Plugin manager started in degraded state") {
		t.Fatal("plugin startup failures must preserve degraded-start behavior")
	}
}
