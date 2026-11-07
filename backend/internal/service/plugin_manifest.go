package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
)

const (
	PluginCapabilityOpenAIOAuthOutbound = "openai.oauth.outbound_transport.v1"
	PluginCapabilityBackendInvoke       = "backend.invoke.v1"
	PluginCapabilityBackendBackground   = "backend.background.v1"
	PluginCapabilityStorageRead         = "storage.read.v1"
	PluginCapabilityStorageWrite        = "storage.write.v1"
	PluginCapabilityUsersCreatedRead    = "users.created.read.v1"
	PluginCapabilityUsersEmailRead      = "users.email.read.v1"
	PluginStateDisabled                 = "disabled"
	PluginStateStarting                 = "starting"
	PluginStateEnabled                  = "enabled"
	PluginStateError                    = "error"
	PluginStateIncompatible             = "incompatible"
	PluginSignatureTrusted              = "trusted"
	PluginSignatureUnsigned             = "unsigned"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)+$`)

var ErrPluginStateChanged = errors.New("插件状态已在其他实例中变化，请刷新后重试")

// PluginManifest 是 .s2plugin 包中可在执行二进制前检查的声明。
type PluginManifest struct {
	SchemaVersion int                      `json:"schema_version"`
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Version       string                   `json:"version"`
	Description   string                   `json:"description,omitempty"`
	Author        string                   `json:"author,omitempty"`
	Requires      PluginRequirements       `json:"requires"`
	Capabilities  []PluginCapability       `json:"capabilities"`
	Runtimes      map[string]PluginRuntime `json:"runtimes"`
	UI            PluginUIManifest         `json:"ui"`
	Backend       PluginBackendManifest    `json:"backend,omitzero"`
	Files         map[string]string        `json:"files"`
}

type PluginRequirements struct {
	Sub2API                   string   `json:"sub2api"`
	RecommendedSub2APIVersion string   `json:"recommended_sub2api_version,omitempty"`
	TestedSub2APIVersions     []string `json:"tested_sub2api_versions,omitempty"`
	PluginProtocol            int      `json:"plugin_protocol"`
	TransportAPI              int      `json:"transport_api"`
	UIBridge                  int      `json:"ui_bridge"`
	BackendAPI                int      `json:"backend_api,omitempty"`
	HostServiceAPI            int      `json:"host_service_api,omitempty"`
}

type PluginCapability struct {
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	AccountType string `json:"account_type"`
}

type PluginRuntime struct {
	Path string `json:"path"`
}

type PluginUIManifest struct {
	Entrypoint string          `json:"entrypoint,omitempty"`
	Entries    []PluginUIEntry `json:"entries,omitempty"`
}

type PluginUIEntry struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Path  string   `json:"path"`
	Mount string   `json:"mount"`
	Roles []string `json:"roles"`
}

type PluginBackendManifest struct {
	Methods []PluginBackendMethod `json:"methods,omitempty"`
}

type PluginBackendMethod struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Access string `json:"access"`
}

type PluginSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// PluginCompatibility 是管理页面展示和启用门禁共同使用的兼容性结论。
type PluginCompatibility struct {
	Compatible         bool   `json:"compatible"`
	Tested             bool   `json:"tested"`
	Status             string `json:"status"`
	Message            string `json:"message"`
	CurrentSub2API     string `json:"current_sub2api_version"`
	RequiredSub2API    string `json:"required_sub2api_version"`
	RecommendedSub2API string `json:"recommended_sub2api_version"`
	PluginProtocol     int    `json:"plugin_protocol"`
	TransportAPI       int    `json:"transport_api"`
	UIBridge           int    `json:"ui_bridge"`
	BackendAPI         int    `json:"backend_api"`
	HostServiceAPI     int    `json:"host_service_api"`
}

type PluginInstallation struct {
	ID              int64               `json:"id"`
	PluginKey       string              `json:"plugin_key"`
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	Description     string              `json:"description"`
	Author          string              `json:"author"`
	Manifest        PluginManifest      `json:"manifest"`
	ArtifactData    []byte              `json:"-"`
	ArtifactPath    string              `json:"-"`
	InstallPath     string              `json:"-"`
	BinaryPath      string              `json:"-"`
	BinarySHA256    string              `json:"binary_sha256"`
	SignatureStatus string              `json:"signature_status"`
	State           string              `json:"state"`
	ConfigEncrypted string              `json:"-"`
	LastError       string              `json:"last_error"`
	InstalledBy     *int64              `json:"installed_by"`
	InstalledAt     time.Time           `json:"installed_at"`
	EnabledAt       *time.Time          `json:"enabled_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	Bindings        []PluginBinding     `json:"bindings"`
	Compatibility   PluginCompatibility `json:"compatibility"`
	RuntimeHealthy  bool                `json:"runtime_healthy"`
	RuntimeMessage  string              `json:"runtime_message"`
}

type PluginBinding struct {
	ID             int64     `json:"id"`
	PluginID       int64     `json:"plugin_id"`
	Capability     string    `json:"capability"`
	Platform       string    `json:"platform"`
	AccountType    string    `json:"account_type"`
	Enabled        bool      `json:"enabled"`
	RolloutPercent int       `json:"rollout_percent"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PluginRepository interface {
	List(ctx context.Context) ([]*PluginInstallation, error)
	GetByID(ctx context.Context, id int64) (*PluginInstallation, error)
	GetByKey(ctx context.Context, key string) (*PluginInstallation, error)
	Install(ctx context.Context, plugin *PluginInstallation, bindings []PluginBinding) (*PluginInstallation, error)
	GetArtifact(ctx context.Context, id int64) ([]byte, error)
	Delete(ctx context.Context, id int64, expectedBinarySHA256 string) error
	BeginEnable(ctx context.Context, id int64, binarySHA256, expectedState string) error
	MarkRuntimeHealthy(ctx context.Context, id int64, binarySHA256, configEncrypted string) error
	UpdateState(ctx context.Context, id int64, state, lastError string, enabledAt *time.Time, expectedBinarySHA256, expectedState string) error
	UpdateConfig(ctx context.Context, id int64, encrypted, expectedBinarySHA256 string) error
	UpdateBindingsAndState(ctx context.Context, pluginID int64, bindings []PluginBinding, state, lastError string, enabledAt *time.Time, expectedState, expectedBinarySHA256 string) error
}

func (m PluginManifest) RuntimeKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func (m PluginManifest) Validate() error {
	if m.SchemaVersion != 1 && m.SchemaVersion != 2 {
		return fmt.Errorf("不支持的插件清单版本: %d", m.SchemaVersion)
	}
	if !pluginIDPattern.MatchString(m.ID) || len(m.ID) > 160 {
		return errors.New("插件 ID 必须是长度不超过 160 的小写命名空间标识")
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 160 {
		return errors.New("插件名称不能为空且不能超过 160 个字符")
	}
	if normalizeSemver(m.Version) == "" {
		return errors.New("插件版本必须是有效的语义化版本")
	}
	if strings.TrimSpace(m.Requires.Sub2API) == "" {
		return errors.New("插件必须声明 requires.sub2api")
	}
	if m.SchemaVersion == 2 {
		return m.validateGeneric()
	}
	if m.Requires.BackendAPI != 0 || m.Requires.HostServiceAPI != 0 || len(m.Backend.Methods) != 0 || len(m.UI.Entries) != 0 {
		return errors.New("旧版插件不能声明通用后台或业务入口")
	}
	if m.Requires.PluginProtocol != pluginv1.ProtocolVersion ||
		m.Requires.TransportAPI != pluginv1.TransportAPIVersion ||
		m.Requires.UIBridge != pluginv1.UIBridgeVersion {
		return errors.New("插件协议、传输 API 或 UI Bridge 版本与当前宿主不兼容")
	}
	if len(m.Capabilities) == 0 {
		return errors.New("插件必须声明至少一个能力")
	}
	for _, capability := range m.Capabilities {
		if capability.ID != PluginCapabilityOpenAIOAuthOutbound || capability.Platform != PlatformOpenAI || capability.AccountType != AccountTypeOAuth {
			return fmt.Errorf("初期仅支持能力 %s", PluginCapabilityOpenAIOAuthOutbound)
		}
	}
	runtimeEntry, ok := m.Runtimes[m.RuntimeKey()]
	if !ok || !safePluginRelativePath(runtimeEntry.Path) {
		return fmt.Errorf("插件不支持当前运行平台 %s", m.RuntimeKey())
	}
	if !safePluginRelativePath(m.UI.Entrypoint) || !strings.HasPrefix(m.UI.Entrypoint, "ui/") {
		return errors.New("插件 UI 入口必须位于 ui/ 目录")
	}
	if len(m.Files) == 0 {
		return errors.New("插件清单必须声明文件哈希")
	}
	for path, hash := range m.Files {
		if !safePluginRelativePath(path) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(hash) {
			return fmt.Errorf("插件文件声明无效: %s", path)
		}
	}
	if _, ok := m.Files[runtimeEntry.Path]; !ok {
		return errors.New("运行时二进制未包含在文件哈希声明中")
	}
	if _, ok := m.Files[m.UI.Entrypoint]; !ok {
		return errors.New("UI 入口未包含在文件哈希声明中")
	}
	return nil
}

func safePluginRelativePath(path string) bool {
	cleaned := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	return cleaned != "" && cleaned != "." && !strings.HasPrefix(cleaned, "/") &&
		!strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, "/../") && cleaned == strings.TrimPrefix(cleaned, "./")
}

func (m PluginManifest) MarshalJSONBytes() ([]byte, error) {
	return json.Marshal(m)
}

func (m PluginManifest) SortedCapabilities() []PluginCapability {
	out := append([]PluginCapability(nil), m.Capabilities...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m PluginManifest) IsGeneric() bool  { return m.SchemaVersion == 2 }
func (m PluginManifest) HasRuntime() bool { return len(m.Runtimes) > 0 }
func (m PluginManifest) HasCapability(id string) bool {
	for _, capability := range m.Capabilities {
		if capability.ID == id {
			return true
		}
	}
	return false
}

var pluginExtensionIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)
var pluginFileHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (m PluginManifest) validateGeneric() error {
	hasUI := m.UI.Entrypoint != "" || len(m.UI.Entries) > 0
	bridge := 0
	if hasUI {
		bridge = pluginv1.ApplicationUIBridgeVersion
	}
	protocol, backend, host := 0, 0, 0
	if m.HasRuntime() {
		protocol, backend, host = pluginv1.ProtocolVersion, pluginv1.BackendAPIVersion, pluginv1.GenericHostServiceAPIVersion
	}
	if m.Requires.PluginProtocol != protocol || m.Requires.TransportAPI != 0 || m.Requires.BackendAPI != backend || m.Requires.HostServiceAPI != host || m.Requires.UIBridge != bridge {
		return errors.New("通用插件的后台、宿主服务或 UI Bridge 版本与声明类型不匹配")
	}
	if !hasUI && !m.HasRuntime() {
		return errors.New("通用插件必须声明页面或后台运行时")
	}
	allowed := map[string]bool{
		PluginCapabilityBackendInvoke: true, PluginCapabilityBackendBackground: true,
		PluginCapabilityStorageRead: true, PluginCapabilityStorageWrite: true,
		PluginCapabilityUsersCreatedRead: true, PluginCapabilityUsersEmailRead: true,
	}
	seen := make(map[string]bool)
	for _, cap := range m.Capabilities {
		if !allowed[cap.ID] || seen[cap.ID] || cap.Platform != "" || cap.AccountType != "" {
			return fmt.Errorf("通用插件能力无效或重复: %s", cap.ID)
		}
		seen[cap.ID] = true
	}
	if !m.HasRuntime() && (len(m.Capabilities) > 0 || len(m.Backend.Methods) > 0) {
		return errors.New("纯前端插件不能申请后台或敏感宿主能力")
	}
	if m.HasRuntime() && !seen[PluginCapabilityBackendInvoke] && !seen[PluginCapabilityBackendBackground] {
		return errors.New("后台运行时必须声明调用或后台工作能力")
	}
	if seen[PluginCapabilityStorageWrite] && !seen[PluginCapabilityStorageRead] {
		return errors.New("写入存储必须同时声明读取存储能力")
	}
	if seen[PluginCapabilityUsersEmailRead] && !seen[PluginCapabilityUsersCreatedRead] {
		return errors.New("读取邮箱必须同时声明用户创建观察能力")
	}
	if len(m.Backend.Methods) > 32 {
		return errors.New("后台方法不能超过 32 项")
	}
	methods := make(map[string]bool)
	for _, method := range m.Backend.Methods {
		if !pluginExtensionIdentifier.MatchString(method.Name) || methods[method.Name] || (method.Role != RoleAdmin && method.Role != RoleUser) || (method.Access != "read" && method.Access != "write") {
			return fmt.Errorf("后台方法的名称、角色或读写级别无效: %s", method.Name)
		}
		methods[method.Name] = true
	}
	if len(methods) > 0 && !seen[PluginCapabilityBackendInvoke] {
		return errors.New("声明后台方法必须申请 backend.invoke.v1")
	}
	if len(m.Files) == 0 {
		return errors.New("插件清单必须声明文件哈希")
	}
	for path, hash := range m.Files {
		if normalized, err := normalizePluginArchivePath(path); err != nil || normalized != path || !pluginFileHashPattern.MatchString(hash) {
			return fmt.Errorf("插件文件声明无效: %s", path)
		}
	}
	if m.HasRuntime() {
		if _, ok := m.Runtimes[m.RuntimeKey()]; !ok {
			return fmt.Errorf("插件不支持当前运行平台 %s", m.RuntimeKey())
		}
		for key, entry := range m.Runtimes {
			if !regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`).MatchString(key) {
				return errors.New("插件运行平台标识无效")
			}
			if normalized, err := normalizePluginArchivePath(entry.Path); err != nil || normalized != entry.Path {
				return errors.New("插件运行时路径无效")
			}
			if _, ok := m.Files[entry.Path]; !ok {
				return errors.New("运行时二进制未包含在文件哈希声明中")
			}
		}
	}
	validUIPath := func(path string) bool {
		normalized, err := normalizePluginArchivePath(path)
		_, declared := m.Files[path]
		return err == nil && normalized == path && strings.HasPrefix(path, "ui/") && declared
	}
	if m.UI.Entrypoint != "" && !validUIPath(m.UI.Entrypoint) {
		return errors.New("管理页面入口必须是已声明哈希的 ui/ 文件")
	}
	entries := make(map[string]bool)
	for _, entry := range m.UI.Entries {
		if !pluginExtensionIdentifier.MatchString(entry.ID) || entries[entry.ID] || strings.TrimSpace(entry.Label) == "" || len(entry.Label) > 160 || !validUIPath(entry.Path) {
			return errors.New("插件业务入口的标识、名称或路径无效")
		}
		entries[entry.ID] = true
		if entry.Mount != "admin" && entry.Mount != "user" && entry.Mount != "shell" {
			return errors.New("插件业务入口挂载位置无效")
		}
		if len(entry.Roles) == 0 || len(entry.Roles) > 2 {
			return errors.New("插件业务入口必须明确声明角色")
		}
		roles := make(map[string]bool)
		for _, role := range entry.Roles {
			if (role != RoleAdmin && role != RoleUser) || roles[role] || (entry.Mount == "admin" && role != RoleAdmin) {
				return errors.New("插件业务入口角色无效或重复")
			}
			roles[role] = true
		}
	}
	return nil
}

// Keep schema 1 serialization unchanged while explicitly representing all four
// backend negotiation versions (including zero) in a schema 2 public manifest.
func (m PluginManifest) MarshalJSON() ([]byte, error) {
	type manifestValue PluginManifest
	if !m.IsGeneric() {
		return json.Marshal(manifestValue(m))
	}
	type requirementsValue PluginRequirements
	type genericRequirements struct {
		requirementsValue
		BackendAPI     int `json:"backend_api"`
		HostServiceAPI int `json:"host_service_api"`
	}
	return json.Marshal(struct {
		manifestValue
		Requires genericRequirements `json:"requires"`
	}{manifestValue: manifestValue(m), Requires: genericRequirements{requirementsValue: requirementsValue(m.Requires), BackendAPI: m.Requires.BackendAPI, HostServiceAPI: m.Requires.HostServiceAPI}})
}
