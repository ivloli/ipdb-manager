package goal

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type GoalPayload struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

func PublishGoal(nacosClient config_client.IConfigClient, system, version string) error {
	group := fmt.Sprintf("ipdb_agent_goal_%s", system)
	dataID := "default"

	payload := GoalPayload{
		Version:   version,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedBy: "ipdb-manager",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal goal: %w", err)
	}

	ok, err := nacosClient.PublishConfig(vo.ConfigParam{
		DataId:  dataID,
		Group:   group,
		Content: string(b),
	})
	if err != nil {
		return fmt.Errorf("publish goal %s/%s: %w", group, dataID, err)
	}
	if !ok {
		return fmt.Errorf("publish goal %s/%s returned false", group, dataID)
	}
	log.Printf("[goal] published system=%s version=%s", system, version)
	return nil
}
