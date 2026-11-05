package config

import (
	"encoding/json"
	"testing"
)

// TestThemeConfigUnmarshalBackwardCompat 锁住 theme 从裸字符串升级为对象后的向后兼容：
// 老 config.json 里 "theme":"dark" 必须读成 {Mode:"dark", 其余默认}，不能报错也不能丢主题档。
// 新维度 glass/background 也应补齐默认（关闭 + none），避免下游拿到零值意外开启。
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
			// 字符串形态应补齐其余维度默认，避免下游拿到空短名 / 误开玻璃背景
			if tc.Preset != defaultThemePreset || tc.Radius != "default" {
				t.Errorf("老字符串未补齐默认: %+v", tc)
			}
			if tc.Glass.On {
				t.Errorf("老字符串不应默认开启玻璃: %+v", tc.Glass)
			}
			if tc.Background.Type != backgroundNone {
				t.Errorf("老字符串背景应兜 none, 得到 %q", tc.Background.Type)
			}
		})
	}
}

// TestThemeGlassUnmarshalLegacyString 锁住 glass 从短名升级为对象后的兼容：
// "surface" 映射成阶段一那组观感，"off"/其它兜默认关闭，对象走常规解析，null 保持零值。
func TestThemeGlassUnmarshalLegacyString(t *testing.T) {
	var surface ThemeConfig
	if err := json.Unmarshal([]byte(`{"glass":"surface"}`), &surface); err != nil {
		t.Fatalf("Unmarshal glass:surface 报错: %v", err)
	}
	if surface.Glass != legacySurfaceGlass() {
		t.Errorf("glass:surface 应映射为旧观感 %+v, 得到 %+v", legacySurfaceGlass(), surface.Glass)
	}

	var off ThemeConfig
	if err := json.Unmarshal([]byte(`{"glass":"off"}`), &off); err != nil {
		t.Fatalf("Unmarshal glass:off 报错: %v", err)
	}
	if off.Glass != defaultGlass() {
		t.Errorf("glass:off 应兜默认关闭 %+v, 得到 %+v", defaultGlass(), off.Glass)
	}

	var obj ThemeConfig
	raw := `{"glass":{"on":true,"material":"acrylic","alpha":60,"blur":0,"border":0,"shadow":18,"noise":120}}`
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("Unmarshal glass 对象报错: %v", err)
	}
	want := ThemeGlass{On: true, Material: "acrylic", Alpha: 60, Blur: 0, Border: 0, Shadow: 18, Noise: 120}
	if obj.Glass != want {
		t.Errorf("glass 对象解析 = %+v, 期望 %+v", obj.Glass, want)
	}
}

// TestThemeConfigUnmarshalObject 新对象形态按字段解析（含 glass/background 对象），合法值不被覆盖。
func TestThemeConfigUnmarshalObject(t *testing.T) {
	raw := `{"mode":"dark","preset":"ocean","primary":"#123456","radius":"large","density":"compact","contrast":"high","motion":"reduced","glass":{"on":true,"material":"frosted","alpha":72,"blur":16,"border":30,"shadow":18},"background":{"type":"image","image":"C:\\x\\bg.png"}}`
	var tc ThemeConfig
	if err := json.Unmarshal([]byte(raw), &tc); err != nil {
		t.Fatalf("Unmarshal 对象报错: %v", err)
	}
	want := ThemeConfig{
		Mode: "dark", Preset: "ocean", Primary: "#123456", Radius: "large",
		Density: "compact", Contrast: "high", Motion: "reduced",
		Glass:      ThemeGlass{On: true, Material: "frosted", Alpha: 72, Blur: 16, Border: 30, Shadow: 18},
		Background: ThemeBackground{Type: "image", Image: `C:\x\bg.png`},
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
	if tc.Preset == "" || tc.Radius == "" || tc.Density == "" || tc.FontSize == "" || tc.Contrast == "" || tc.Motion == "" || tc.Harmony == "" {
		t.Errorf("空维度未补齐默认: %+v", tc)
	}
	if tc.Glass.Material != glassMaterialFrosted {
		t.Errorf("空 glass 应兜 frosted, 得到 %q", tc.Glass.Material)
	}
	if tc.Background.Type != backgroundNone {
		t.Errorf("空 background 应兜 none, 得到 %q", tc.Background.Type)
	}
	if tc.Harmony != defaultThemeHarmony {
		t.Errorf("非法 harmony 应兑 analogous, 得到 %q", tc.Harmony)
	}
	if tc.FontSize != defaultThemeFontSize {
		t.Errorf("非法/空 fontSize 应兑 default, 得到 %q", tc.FontSize)
	}
	// 二次归一不改动已合法的值
	keep := ThemeConfig{Mode: ThemeModeDark, Preset: "forest", Harmony: "triadic", Radius: "small", Density: "loose", FontSize: "large", Contrast: "high", Motion: "reduced", Glass: ThemeGlass{On: true, Material: "acrylic", Alpha: 60, Blur: 20, Border: 40, Shadow: 30, Noise: 100}, Background: ThemeBackground{Type: "mesh"}, Primary: "#abc"}
	normalizeThemeConfig(&keep)
	if keep != (ThemeConfig{Mode: ThemeModeDark, Preset: "forest", Harmony: "triadic", Radius: "small", Density: "loose", FontSize: "large", Contrast: "high", Motion: "reduced", Glass: ThemeGlass{On: true, Material: "acrylic", Alpha: 60, Blur: 20, Border: 40, Shadow: 30, Noise: 100}, Background: ThemeBackground{Type: "mesh"}, Primary: "#abc"}) {
		t.Errorf("合法值被改动: %+v", keep)
	}
}

// TestNormalizeThemeGlassClamps 越界玻璃参数夹回区间，非法材质兜 frosted。
func TestNormalizeThemeGlassClamps(t *testing.T) {
	g := ThemeGlass{Material: "bogus", Alpha: 999, Blur: -5, Border: 200, Shadow: 90, Noise: 500}
	normalizeThemeGlass(&g)
	if g.Material != glassMaterialFrosted {
		t.Errorf("非法 material 应兜 frosted, 得到 %q", g.Material)
	}
	if g.Alpha != 100 || g.Blur != 0 || g.Border != 100 || g.Shadow != 50 || g.Noise != 120 {
		t.Errorf("越界参数未夹回: %+v", g)
	}
}

// TestNormalizeThemeBackground 非 image 档清空残留路径，非法 type 兜 none。
func TestNormalizeThemeBackground(t *testing.T) {
	b := ThemeBackground{Type: "bogus", Image: "C:\\x\\bg.png"}
	normalizeThemeBackground(&b)
	if b.Type != backgroundNone || b.Image != "" {
		t.Errorf("非法 type 应兜 none 且清空 image: %+v", b)
	}

	img := ThemeBackground{Type: backgroundImage, Image: "  C:\\x\\bg.png  "}
	normalizeThemeBackground(&img)
	if img.Image != `C:\x\bg.png` {
		t.Errorf("image 档应 trim 保留路径, 得到 %q", img.Image)
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
