//go:build !windows

package browser

// space 非 Windows 平台的占位类型（Manager 需要在所有平台编得过）。
type space struct{}

// Open 非 Windows 平台永远返回不支持。
func (m *Manager) Open(opts Options) error { return ErrUnsupported }

// NewTab 非 Windows 平台永远返回不支持。
func (m *Manager) NewTab(spaceID, url string, activate bool) (string, error) {
	return "", ErrUnsupported
}

// CloseTab 非 Windows 平台永远返回不支持。
func (m *Manager) CloseTab(spaceID, tabID string) error { return ErrUnsupported }

// ActivateTab 非 Windows 平台永远返回不支持。
func (m *Manager) ActivateTab(spaceID, tabID string) error { return ErrUnsupported }

// RestoreTabs 非 Windows 平台永远返回不支持。
func (m *Manager) RestoreTabs(spaceID string, urls []string, active int) error {
	return ErrUnsupported
}

// SetVisible 非 Windows 平台永远返回不支持。
func (m *Manager) SetVisible(spaceID string, visible bool) error { return ErrUnsupported }

// Navigate 非 Windows 平台永远返回不支持。
func (m *Manager) Navigate(id, url string) error { return ErrUnsupported }

// NavigateTab 非 Windows 平台永远返回不支持。
func (m *Manager) NavigateTab(spaceID, tabID, url string) error { return ErrUnsupported }

// GoBack 非 Windows 平台永远返回不支持。
func (m *Manager) GoBack(spaceID string) error { return ErrUnsupported }

// GoForward 非 Windows 平台永远返回不支持。
func (m *Manager) GoForward(spaceID string) error { return ErrUnsupported }

// Reload 非 Windows 平台永远返回不支持。
func (m *Manager) Reload(spaceID string) error { return ErrUnsupported }

// Stop 非 Windows 平台永远返回不支持。
func (m *Manager) Stop(spaceID string) error { return ErrUnsupported }

// SpaceState 非 Windows 平台永远返回不支持。
func (m *Manager) SpaceState(spaceID string) (SpaceInfo, error) { return SpaceInfo{}, ErrUnsupported }

// SpaceStates 非 Windows 平台返回空列表。
func (m *Manager) SpaceStates() []SpaceInfo { return nil }

// FocusShell 非 Windows 平台永远返回不支持。
func (m *Manager) FocusShell(spaceID string) error { return ErrUnsupported }

// SetBounds 非 Windows 平台永远返回不支持。
func (m *Manager) SetBounds(id string, r Rect) error { return ErrUnsupported }

// DevTools 非 Windows 平台永远返回不支持。
func (m *Manager) DevTools(id string) error { return ErrUnsupported }

// Eval 非 Windows 平台永远返回不支持。
func (m *Manager) Eval(id, script string) error { return ErrUnsupported }

// Close 非 Windows 平台无操作。
func (m *Manager) Close(id string) {}

// CloseAll 非 Windows 平台无操作。
func (m *Manager) CloseAll() {}

// Stats 非 Windows 平台只回报空间数。
func (m *Manager) Stats() Stat { return Stat{Spaces: m.count()} }

// DetectRuntimeVersion 非 Windows 平台没有 WebView2，返回空串。
func DetectRuntimeVersion() string { return "" }

// FindWindowByTitle 非 Windows 平台永远返回不支持。
func FindWindowByTitle(title string) (uintptr, error) { return 0, ErrUnsupported }
