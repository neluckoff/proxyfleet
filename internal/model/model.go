package model

import "time"

type State struct {
	Settings *Settings    `json:"settings,omitempty"`
	Nodes    []*Node      `json:"nodes"`
	Users    []*ProxyUser `json:"users"`
	Events   []*Event     `json:"events"`
}

type Settings struct {
	AdminUsername     string    `json:"adminUsername"`
	AdminPasswordHash string    `json:"adminPasswordHash"`
	SessionSecret     string    `json:"sessionSecret"`
	PublicURL         string    `json:"publicUrl"`
	CreatedAt         time.Time `json:"createdAt"`
}

type Node struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Region              string    `json:"region"`
	AdvertisedHost      string    `json:"advertisedHost"`
	HTTPPort            int       `json:"httpPort"`
	SOCKSPort           int       `json:"socksPort"`
	AllowPrivateTargets bool      `json:"allowPrivateTargets"`
	EnrollTokenHash     string    `json:"enrollTokenHash,omitempty"`
	AgentTokenHash      string    `json:"agentTokenHash,omitempty"`
	ConfigVersion       int64     `json:"configVersion"`
	CreatedAt           time.Time `json:"createdAt"`
	LastSeenAt          time.Time `json:"lastSeenAt,omitempty"`
	AgentVersion        string    `json:"agentVersion,omitempty"`
	PublicIP            string    `json:"publicIp,omitempty"`
	TelegramOK          bool      `json:"telegramOk"`
	TelegramLatencyMS   int64     `json:"telegramLatencyMs"`
	LastError           string    `json:"lastError,omitempty"`
	UptimeSeconds       int64     `json:"uptimeSeconds"`
	ActiveConnections   int64     `json:"activeConnections"`
	BytesUp             int64     `json:"bytesUp"`
	BytesDown           int64     `json:"bytesDown"`
}

type ProxyUser struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
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

type AgentUser struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
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
	AgentVersion      string `json:"agentVersion"`
	PublicIP          string `json:"publicIp"`
	TelegramOK        bool   `json:"telegramOk"`
	TelegramLatencyMS int64  `json:"telegramLatencyMs"`
	LastError         string `json:"lastError,omitempty"`
	UptimeSeconds     int64  `json:"uptimeSeconds"`
	ActiveConnections int64  `json:"activeConnections"`
	BytesUp           int64  `json:"bytesUp"`
	BytesDown         int64  `json:"bytesDown"`
}
