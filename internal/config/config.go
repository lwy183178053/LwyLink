package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
)

const envPrefix = "LWYLINK_"

type ControlConfig struct {
	HTTPAddr                       string `json:"httpAddr"`
	MySQLDSN                       string `json:"mysqlDsn"`
	RedisAddr                      string `json:"redisAddr"`
	RedisPassword                  string `json:"redisPassword"`
	TokenSecret                    string `json:"tokenSecret"`
	AdminToken                     string `json:"adminToken"`
	ClientReleaseDir               string `json:"clientReleaseDir"`
	NodeHealthCheckIntervalSeconds int    `json:"nodeHealthCheckIntervalSeconds"`
	RoomCleanupIntervalSeconds     int    `json:"roomCleanupIntervalSeconds"`
	RoomEmptyGraceSeconds          int    `json:"roomEmptyGraceSeconds"`
	RoomNetworkBase                string `json:"roomNetworkBase"`
	RoomSubnetPrefix               int    `json:"roomSubnetPrefix"`
}

type NodeConfig struct {
	OverlayAddr       string   `json:"overlayAddr"`
	MySQLDSN          string   `json:"mysqlDsn"`
	RedisAddr         string   `json:"redisAddr"`
	RedisPassword     string   `json:"redisPassword"`
	TokenSecret       string   `json:"tokenSecret"`
	OverlayPrivateKey string   `json:"overlayPrivateKey"`
	OverlayPublicKey  string   `json:"-"`
	RoomNetworkBase   string   `json:"roomNetworkBase"`
	RoomSubnetPrefix  int      `json:"roomSubnetPrefix"`
	DefaultMTU        int      `json:"defaultMTU"`
	DefaultDNS        []string `json:"defaultDNS"`
}

func LoadControlConfig(path string) (ControlConfig, error) {
	var cfg ControlConfig
	if err := loadJSONConfig(path, &cfg); err != nil {
		return cfg, err
	}
	applyControlDefaults(&cfg)
	applyControlEnvOverrides(&cfg)

	changed := false
	if isPlaceholderValue(cfg.TokenSecret) || cfg.TokenSecret == "change-me-to-a-long-random-secret" {
		secret, err := randomSecret(48)
		if err != nil {
			return cfg, fmt.Errorf("generate tokenSecret: %w", err)
		}
		cfg.TokenSecret = secret
		changed = true
	}
	if isPlaceholderValue(cfg.AdminToken) {
		secret, err := randomSecret(32)
		if err != nil {
			return cfg, fmt.Errorf("generate adminToken: %w", err)
		}
		cfg.AdminToken = secret
		changed = true
	}
	if changed && strings.TrimSpace(path) != "" {
		if err := writeJSONConfig(path, cfg); err != nil {
			return cfg, fmt.Errorf("write generated control secrets: %w", err)
		}
	}
	return cfg, nil
}

func LoadNodeConfig(path string) (NodeConfig, error) {
	var cfg NodeConfig
	if err := loadJSONConfig(path, &cfg); err != nil {
		return cfg, err
	}
	applyNodeDefaults(&cfg)
	applyNodeEnvOverrides(&cfg)

	changed := false
	if isPlaceholderValue(cfg.OverlayPrivateKey) {
		privateKey, publicKey, err := tcpx.GenerateStaticKeyBase64()
		if err != nil {
			return cfg, fmt.Errorf("generate overlay private key: %w", err)
		}
		cfg.OverlayPrivateKey = privateKey
		cfg.OverlayPublicKey = publicKey
		changed = true
	} else {
		publicKey, err := tcpx.PublicKeyBase64FromPrivate(cfg.OverlayPrivateKey)
		if err != nil {
			return cfg, fmt.Errorf("parse overlayPrivateKey: %w", err)
		}
		cfg.OverlayPublicKey = publicKey
	}
	if changed && strings.TrimSpace(path) != "" {
		if err := writeJSONConfig(path, cfg); err != nil {
			return cfg, fmt.Errorf("write generated node key: %w", err)
		}
	}
	return cfg, nil
}

func ValidateControlConfig(cfg ControlConfig) error {
	if isPlaceholderValue(cfg.TokenSecret) || cfg.TokenSecret == "change-me-to-a-long-random-secret" {
		return fmt.Errorf("tokenSecret must be changed before starting the control plane")
	}
	if isPlaceholderValue(cfg.AdminToken) {
		return fmt.Errorf("adminToken must be changed before starting the control plane")
	}
	return nil
}

func ValidateNodeConfig(cfg NodeConfig) error {
	if isPlaceholderValue(cfg.TokenSecret) || cfg.TokenSecret == "change-me-to-a-long-random-secret" {
		return fmt.Errorf("tokenSecret must be copied from the control plane before starting the room node")
	}
	return nil
}

func loadJSONConfig(path string, cfg any) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return nil
}

func writeJSONConfig(path string, cfg any) error {
	mode := os.FileMode(0600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, mode)
}

func isPlaceholderValue(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.HasPrefix(value, "CHANGE_ME")
}

func randomSecret(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func applyControlDefaults(cfg *ControlConfig) {
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = "127.0.0.1:8080"
	}
	if cfg.MySQLDSN == "" {
		cfg.MySQLDSN = "lwylink:lwylink@tcp(127.0.0.1:3306)/lwylink?parseTime=true&charset=utf8mb4&loc=Local"
	}
	if cfg.RedisAddr == "" {
		cfg.RedisAddr = "127.0.0.1:6379"
	}
	if cfg.ClientReleaseDir == "" {
		cfg.ClientReleaseDir = "data/client-releases"
	}
	if cfg.NodeHealthCheckIntervalSeconds <= 0 {
		cfg.NodeHealthCheckIntervalSeconds = 30
	}
	if cfg.RoomCleanupIntervalSeconds <= 0 {
		cfg.RoomCleanupIntervalSeconds = 10
	}
	if cfg.RoomEmptyGraceSeconds <= 0 {
		cfg.RoomEmptyGraceSeconds = 20
	}
	if cfg.RoomNetworkBase == "" {
		cfg.RoomNetworkBase = "172.16.0.0/16"
	}
	if cfg.RoomSubnetPrefix == 0 {
		cfg.RoomSubnetPrefix = 24
	}
}

func applyNodeDefaults(cfg *NodeConfig) {
	if cfg.OverlayAddr == "" {
		cfg.OverlayAddr = "0.0.0.0:7777"
	}
	if cfg.MySQLDSN == "" {
		cfg.MySQLDSN = "lwylink:lwylink@tcp(127.0.0.1:3306)/lwylink?parseTime=true&charset=utf8mb4&loc=Local"
	}
	if cfg.RedisAddr == "" {
		cfg.RedisAddr = "127.0.0.1:6379"
	}
	if cfg.RoomNetworkBase == "" {
		cfg.RoomNetworkBase = "172.16.0.0/16"
	}
	if cfg.RoomSubnetPrefix == 0 {
		cfg.RoomSubnetPrefix = 24
	}
	if cfg.DefaultMTU <= 0 {
		cfg.DefaultMTU = 1300
	}
	if len(cfg.DefaultDNS) == 0 {
		cfg.DefaultDNS = []string{"223.5.5.5", "119.29.29.29"}
	}
}

func applyControlEnvOverrides(cfg *ControlConfig) {
	if v := os.Getenv(envName("HTTP_ADDR")); v != "" {
		cfg.HTTPAddr = v
	}
	applySharedEnvOverrides(&cfg.MySQLDSN, &cfg.RedisAddr, &cfg.RedisPassword, &cfg.TokenSecret, &cfg.RoomNetworkBase, &cfg.RoomSubnetPrefix)
	if v := os.Getenv(envName("ADMIN_TOKEN")); v != "" {
		cfg.AdminToken = v
	}
	if v := os.Getenv(envName("CLIENT_RELEASE_DIR")); v != "" {
		cfg.ClientReleaseDir = v
	}
	applyEnvInt(envName("NODE_HEALTH_INTERVAL_SECONDS"), &cfg.NodeHealthCheckIntervalSeconds)
	applyEnvInt(envName("ROOM_CLEANUP_INTERVAL_SECONDS"), &cfg.RoomCleanupIntervalSeconds)
	applyEnvInt(envName("ROOM_EMPTY_GRACE_SECONDS"), &cfg.RoomEmptyGraceSeconds)
}

func applyNodeEnvOverrides(cfg *NodeConfig) {
	if v := os.Getenv(envName("OVERLAY_ADDR")); v != "" {
		cfg.OverlayAddr = v
	}
	applySharedEnvOverrides(&cfg.MySQLDSN, &cfg.RedisAddr, &cfg.RedisPassword, &cfg.TokenSecret, &cfg.RoomNetworkBase, &cfg.RoomSubnetPrefix)
	if v := os.Getenv(envName("OVERLAY_PRIVATE_KEY")); v != "" {
		cfg.OverlayPrivateKey = v
	}
	applyEnvInt(envName("DEFAULT_MTU"), &cfg.DefaultMTU)
	if v := os.Getenv(envName("DEFAULT_DNS")); v != "" {
		cfg.DefaultDNS = splitCSV(v)
	}
}

func applySharedEnvOverrides(mysqlDSN, redisAddr, redisPassword, tokenSecret, roomNetworkBase *string, roomSubnetPrefix *int) {
	if v := os.Getenv(envName("MYSQL_DSN")); v != "" {
		*mysqlDSN = v
	}
	if v := os.Getenv(envName("REDIS_ADDR")); v != "" {
		*redisAddr = v
	}
	if v := os.Getenv(envName("REDIS_PASSWORD")); v != "" {
		*redisPassword = v
	}
	if v := os.Getenv(envName("TOKEN_SECRET")); v != "" {
		*tokenSecret = v
	}
	if v := os.Getenv(envName("ROOM_NETWORK_BASE")); v != "" {
		*roomNetworkBase = v
	}
	if v := os.Getenv(envName("ROOM_SUBNET_PREFIX")); v != "" {
		applyIntValue(v, roomSubnetPrefix)
	}
}

func envName(name string) string {
	return envPrefix + name
}

func applyEnvInt(name string, target *int) {
	if v := os.Getenv(name); v != "" {
		applyIntValue(v, target)
	}
}

func applyIntValue(value string, target *int) {
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = parsed
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
