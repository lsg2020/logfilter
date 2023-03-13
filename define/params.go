package define

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type AgentParams struct {
	WebSocketAddr string `json:"web_socket_addr"`
	LogPath       string `json:"log_path"`
}

func (ap *AgentParams) ToString() (string, error) {
	str, err := json.Marshal(ap)
	if err != nil {
		return "", fmt.Errorf("json marshal failed, %w", err)
	}
	return base64.StdEncoding.EncodeToString(str), nil
}

func (ap *AgentParams) FromString(str string) error {
	buf, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return fmt.Errorf("invalid param, %w", err)
	}
	return json.Unmarshal(buf, ap)
}
