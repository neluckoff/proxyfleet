package model

import "time"

type State struct {
	Settings         *Settings         `json:"settings,omitempty"`
	Nodes            []*Node           `json:"nodes"`
	Users            []*ProxyUser      `json:"users"`
	Events           []*Event          `json:"events"`
	Activity         []*ActivitySample `json:"activity,omitempty"`
	Traffic          []*TrafficSample  `json:"traffic,omitempty"`
	TrafficBytesUp   int64             `json:"trafficBytesUp,omitempty"`
	TrafficBytesDown int64             `json:"trafficBytesDown,omitempty"`
}

type Settings struct {
	AdminUsername       string    `json:"adminUsername"`
	AdminPasswordHash   string    `json:"adminPasswordHash"`
	SessionSecret       string    `json:"sessionSecret"`
	PublicURL           string    `json:"publicUrl"`
	SystemName          string    `json:"systemName,omitempty"`
	LogoDataURL         string    `json:"logoDataUrl,omitempty"`
	TelegramEnabled     bool      `json:"telegramEnabled,omitempty"`
	TelegramBotToken    string    `json:"telegramBotToken,omitempty"`
	TelegramBotUsername string    `json:"telegramBotUsername,omitempty"`
	TelegramChatID      string    `json:"telegramChatId,omitempty"`
	TelegramProxyURL    string    `json:"telegramProxyUrl,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

type Node struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Region               string    `json:"region"`
	AdvertisedHost       string    `json:"advertisedHost"`
	HTTPPort             int       `json:"httpPort"`
	SOCKSPort            int       `json:"socksPort"`
	AllowPrivateTargets  bool      `json:"allowPrivateTargets"`
	EnrollTokenHash      string    `json:"enrollTokenHash,omitempty"`
	AgentTokenHash       string    `json:"agentTokenHash,omitempty"`
	ConfigVersion        int64     `json:"configVersion"`
	CreatedAt            time.Time `json:"createdAt"`
	LastSeenAt           time.Time `json:"lastSeenAt,omitempty"`
	AgentVersion         string    `json:"agentVersion,omitempty"`
	PublicIP             string    `json:"publicIp,omitempty"`
	TelegramOK           bool      `json:"telegramOk"`
	TelegramLatencyMS    int64     `json:"telegramLatencyMs"`
	LastError            string    `json:"lastError,omitempty"`
	UptimeSeconds        int64     `json:"uptimeSeconds"`
	ActiveConnections    int64     `json:"activeConnections"`
	BytesUp              int64     `json:"bytesUp"`
	BytesDown            int64     `json:"bytesDown"`
	MemoryAllocBytes     int64     `json:"memoryAllocBytes,omitempty"`
	MemorySysBytes       int64     `json:"memorySysBytes,omitempty"`
	Goroutines           int       `json:"goroutines,omitempty"`
	HostMemoryUsedBytes  int64     `json:"hostMemoryUsedBytes,omitempty"`
	HostMemoryTotalBytes int64     `json:"hostMemoryTotalBytes,omitempty"`
	LoadOne              float64   `json:"loadOne,omitempty"`
	OfflineNotified      bool      `json:"offlineNotified,omitempty"`
}

type ProxyUser struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	Protocol     string    `json:"protocol,omitempty"`
	NodeIDs      []string  `json:"nodeIds"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	NodeID    string         `json:"nodeId,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type ActivitySample struct {
	RecordedAt        time.Time `json:"recordedAt"`
	OnlineNodes       int       `json:"onlineNodes"`
	ActiveConnections int64     `json:"activeConnections"`
}

type TrafficSample struct {
	RecordedAt time.Time `json:"recordedAt"`
	BytesUp    int64     `json:"bytesUp"`
	BytesDown  int64     `json:"bytesDown"`
}

type AgentUser struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Protocol     string `json:"protocol,omitempty"`
	Enabled      bool   `json:"enabled"`
}

type AgentConfig struct {
	Version             int64       `json:"version"`
	NodeName            string      `json:"nodeName"`
	HTTPPort            int         `json:"httpPort"`
	SOCKSPort           int         `json:"socksPort"`
	AllowPrivateTargets bool        `json:"allowPrivateTargets"`
	Users               []AgentUser `json:"users"`
}

type Heartbeat struct {
	AgentVersion         string  `json:"agentVersion"`
	PublicIP             string  `json:"publicIp"`
	TelegramOK           bool    `json:"telegramOk"`
	TelegramLatencyMS    int64   `json:"telegramLatencyMs"`
	LastError            string  `json:"lastError,omitempty"`
	UptimeSeconds        int64   `json:"uptimeSeconds"`
	ActiveConnections    int64   `json:"activeConnections"`
	BytesUp              int64   `json:"bytesUp"`
	BytesDown            int64   `json:"bytesDown"`
	MemoryAllocBytes     int64   `json:"memoryAllocBytes"`
	MemorySysBytes       int64   `json:"memorySysBytes"`
	Goroutines           int     `json:"goroutines"`
	HostMemoryUsedBytes  int64   `json:"hostMemoryUsedBytes"`
	HostMemoryTotalBytes int64   `json:"hostMemoryTotalBytes"`
	LoadOne              float64 `json:"loadOne"`
}
