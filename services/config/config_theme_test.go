package config

import (
	"encoding/json"
	"testing"
)

// TestThemeConfigUnmarshalBackwardCompat 锁住 theme 从裸字符串升级为对象后的向后兼容：
// 老 config.json 里 "theme":"dark" 必须读成 {Mode:"dark", 其余默认}，不能报错也不能丢主题档。
func TestThemeConfigUnmarshalBackwardCompat(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string // 期望的 mode
	}{
		{"legacy-light", `"light"`, ThemeModeLight},
		{"legacy-dark", `"dark"`, ThemeModeDark},
		{"legacy-auto", `"auto"`, ThemeModeAuto},
		{"legacy-garbage", `"weird"`, ThemeModeLight}, // 非法旧值兜成 light
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var tc ThemeConfig
			if err := json.Unmarshal([]byte(c.json), &tc); err != nil {
				t.Fatalf("Unmarshal(%s) 报错: %v", c.json, err)
			}
			if tc.Mode != c.want {
				t.Errorf("mode = %q, 期望 %q", tc.Mode, c.want)
			}
			// 字符串形态应补齐其余维度默认，避免下游拿到空短名
			if tc.Preset != defaultThemePreset || tc.Radius != "default" || tc.Glass != "off" {
				t.Errorf("老字符串未补齐默认: %+v", tc)
			}
		})
	}
}

// TestThemeConfigUnmarshalObject 新对象形态按字段解析，用户填的合法值不被覆盖。
func TestThemeConfigUnmarshalObject(t *testing.T) {
	raw := `{"mode":"dark","preset":"ocean","primary":"#123456","radius":"large","density":"compact","contrast":"high","motion":"reduced","glass":"surface"}`
	var tc ThemeConfig
	if err := json.Unmarshal([]byte(raw), &tc); err != nil {
		t.Fatalf("Unmarshal 对象报错: %v", err)
	}
	want := ThemeConfig{
		Mode: "dark", Preset: "ocean", Primary: "#123456", Radius: "large",
		Density: "compact", Contrast: "high", Motion: "reduced", Glass: "surface",
	}
	if tc != want {
		t.Errorf("对象解析 = %+v, 期望 %+v", tc, want)
	}
}

// TestThemeConfigUnmarshalNull null 应回落零值（随后 normalize 兜默认），不报错。
func TestThemeConfigUnmarshalNull(t *testing.T) {
	var tc ThemeConfig
	if err := json.Unmarshal([]byte(`null`), &tc); err != nil {
		t.Fatalf("Unmarshal(null) 报错: %v", err)
	}
	if tc != (ThemeConfig{}) {
		t.Errorf("null 应为零值, 得到 %+v", tc)
	}
}

// TestThemeConfigMarshalRoundTrip 对象序列化回可读 JSON，字段名带 json tag（前端据此消费）。
func TestThemeConfigMarshalRoundTrip(t *testing.T) {
	data, err := json.Marshal(defaultThemeConfig())
	if err != nil {
		t.Fatalf("Marshal 报错: %v", err)
	}
	var back ThemeConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("回读报错: %v (%s)", err, data)
	}
	if back != defaultThemeConfig() {
		t.Errorf("round-trip 不一致: %s -> %+v", data, back)
	}
}

// TestNormalizeThemeConfig 空/非法值兜默认，合法值原样保留（idempotent）。
func TestNormalizeThemeConfig(t *testing.T) {
	tc := ThemeConfig{Mode: "bogus"}
	normalizeThemeConfig(&tc)
	if tc.Mode != ThemeModeLight {
		t.Errorf("非法 mode 应兜 light, 得到 %q", tc.Mode)
	}
	if tc.Preset == "" || tc.Radius == "" || tc.Density == "" || tc.FontSize == "" || tc.Contrast == "" || tc.Motion == "" || tc.Glass == "" || tc.Harmony == "" {
		t.Errorf("空维度未补齐默认: %+v", tc)
	}
	if tc.Harmony != defaultThemeHarmony {
		t.Errorf("非法 harmony 应兑 analogous, 得到 %q", tc.Harmony)
	}
	if tc.FontSize != defaultThemeFontSize {
		t.Errorf("非法/空 fontSize 应兑 default, 得到 %q", tc.FontSize)
	}
	// 二次归一不改动已合法的值
	keep := ThemeConfig{Mode: ThemeModeDark, Preset: "forest", Harmony: "triadic", Radius: "small", Density: "loose", FontSize: "large", Contrast: "high", Motion: "reduced", Glass: "off", Primary: "#abc"}
	normalizeThemeConfig(&keep)
	if keep != (ThemeConfig{Mode: ThemeModeDark, Preset: "forest", Harmony: "triadic", Radius: "small", Density: "loose", FontSize: "large", Contrast: "high", Motion: "reduced", Glass: "off", Primary: "#abc"}) {
		t.Errorf("合法值被改动: %+v", keep)
	}
}

// TestLoadLegacyThemeInFullConfig 端到端：老配置里 general.theme 为裸字符串，整份 AppConfig 能读且 theme 落到对象。
func TestLoadLegacyThemeInFullConfig(t *testing.T) {
	raw := `{"general":{"appName":"Tools","theme":"dark"}}`
	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("整份配置读老 theme 字符串报错: %v", err)
	}
	normalizeConfig(&cfg)
	if cfg.General.Theme.Mode != ThemeModeDark {
		t.Errorf("General.Theme.Mode = %q, 期望 dark", cfg.General.Theme.Mode)
	}
}
