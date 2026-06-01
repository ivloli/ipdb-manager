package goal

import (
	"encoding/json"
	"testing"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type mockConfigClient struct {
	published []vo.ConfigParam
}

func (m *mockConfigClient) PublishConfig(param vo.ConfigParam) (bool, error) {
	m.published = append(m.published, param)
	return true, nil
}

func (m *mockConfigClient) GetConfig(_ vo.ConfigParam) (string, error)    { return "", nil }
func (m *mockConfigClient) DeleteConfig(_ vo.ConfigParam) (bool, error)   { return true, nil }
func (m *mockConfigClient) ListenConfig(_ vo.ConfigParam) error           { return nil }
func (m *mockConfigClient) CancelListenConfig(_ vo.ConfigParam) error     { return nil }
func (m *mockConfigClient) SearchConfig(_ vo.SearchConfigParam) (*model.ConfigPage, error) {
	return nil, nil
}
func (m *mockConfigClient) CloseClient()                                  {}

func TestPublishGoal(t *testing.T) {
	mock := &mockConfigClient{}
	err := PublishGoal(mock, "probe", "v3.16.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.published) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(mock.published))
	}

	p := mock.published[0]
	if p.Group != "ipdb_agent_goal_probe" {
		t.Fatalf("expected group=ipdb_agent_goal_probe, got %s", p.Group)
	}
	if p.DataId != "default" {
		t.Fatalf("expected dataId=default, got %s", p.DataId)
	}

	var payload GoalPayload
	if err := json.Unmarshal([]byte(p.Content), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Version != "v3.16.0" {
		t.Fatalf("expected version=v3.16.0, got %s", payload.Version)
	}
	if payload.UpdatedBy != "ipdb-manager" {
		t.Fatalf("expected updated_by=ipdb-manager, got %s", payload.UpdatedBy)
	}
}
