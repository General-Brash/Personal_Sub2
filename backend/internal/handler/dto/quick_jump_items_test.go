package dto

import "testing"

func TestParseQuickJumpItems(t *testing.T) {
	t.Run("空值与非法 JSON 返回空切片而不是 nil", func(t *testing.T) {
		for _, raw := range []string{"", "  ", "[]", "not-json", "{}"} {
			items := ParseQuickJumpItems(raw)
			if items == nil {
				t.Fatalf("ParseQuickJumpItems(%q) 返回 nil，前端会拿到 null", raw)
			}
			if len(items) != 0 {
				t.Fatalf("ParseQuickJumpItems(%q) = %d 条，want 0", raw, len(items))
			}
		}
	})

	t.Run("完整字段往返", func(t *testing.T) {
		raw := `[{"id":"forum","label":"社区论坛","icon_svg":"<svg/>","url":"https://forum.example.com","visibility":"user","sort_order":2}]`
		items := ParseQuickJumpItems(raw)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		got := items[0]
		if got.ID != "forum" || got.Label != "社区论坛" || got.IconSVG != "<svg/>" ||
			got.URL != "https://forum.example.com" || got.Visibility != "user" || got.SortOrder != 2 {
			t.Fatalf("字段解析不完整: %+v", got)
		}
	})
}

// TestParseUserVisibleQuickJumpItems 守护公开接口只返回明确标记为 user 的跳转项。
func TestParseUserVisibleQuickJumpItems(t *testing.T) {
	raw := `[
		{"id":"a","label":"用户项","url":"https://a.example.com","visibility":"user","sort_order":0},
		{"id":"b","label":"管理项","url":"https://b.example.com","visibility":"admin","sort_order":1},
		{"id":"c","label":"未标注","url":"https://c.example.com","visibility":"","sort_order":2},
		{"id":"d","label":"未知可见性","url":"https://d.example.com","visibility":"staff","sort_order":3}
	]`
	items := ParseUserVisibleQuickJumpItems(raw)
	if len(items) != 1 {
		t.Fatalf("got %d items, want only the explicitly user-visible item", len(items))
	}
	if items[0].ID != "a" || items[0].Visibility != "user" {
		t.Fatalf("unexpected public item: %+v", items[0])
	}

	if got := ParseUserVisibleQuickJumpItems(""); got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回空切片，got %#v", got)
	}
}
