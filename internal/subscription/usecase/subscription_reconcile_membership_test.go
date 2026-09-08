package usecase

import (
	"context"
	"reflect"
	"sort"
	"testing"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

type membershipNodeRepo struct {
	*mockNodeRepo
	nodes []*nodeDomain.Node
}

func (r *membershipNodeRepo) ListActiveNodes(context.Context) ([]*nodeDomain.Node, error) {
	return r.nodes, nil
}

type membershipAccountReader struct {
	accounts []*contract.AccountInfo
}

func (r *membershipAccountReader) ListActiveAccountInfos(context.Context) ([]*contract.AccountInfo, error) {
	return r.accounts, nil
}

type membershipAgentClient struct {
	agent.NodeClient
	users map[string][]*pb.UserInfo
}

func (c *membershipAgentClient) ListUsers(_ context.Context, tag string) ([]*pb.UserInfo, error) {
	return c.users[tag], nil
}
func (c *membershipAgentClient) Close() error { return nil }

type membershipNodeUsecase struct {
	nodeUsecase.NodeUsecase
	client  *membershipAgentClient
	added   []string
	removed []string
}

func (u *membershipNodeUsecase) GetNodeClient(context.Context, uint) (agent.NodeClient, error) {
	return u.client, nil
}
func (u *membershipNodeUsecase) AddUserViaAgent(_ context.Context, _ *nodeDomain.Node, tag, email, _, _, _, _ string) error {
	u.added = append(u.added, tag+":"+email)
	return nil
}
func (u *membershipNodeUsecase) RemoveUserViaAgent(_ context.Context, _ *nodeDomain.Node, tag, email string) error {
	u.removed = append(u.removed, tag+":"+email)
	return nil
}

func TestReconcileUsers_MembershipIsScopedToInbound(t *testing.T) {
	nodeUC := &membershipNodeUsecase{client: &membershipAgentClient{users: map[string][]*pb.UserInfo{
		"one": {{Email: "user_present"}, {Email: "user_present"}, {Email: "user_ghost"}, {Email: "operator"}},
		"two": {{Email: "manual_present"}},
	}}}
	uc := &subscriptionUsecase{
		subRepo: newMockSubscriptionRepo(),
		nodeRepo: &membershipNodeRepo{nodes: []*nodeDomain.Node{{ID: 1, Inbounds: []nodeDomain.Inbound{
			{ID: 1, Tag: "one", Protocol: "vless"},
			{ID: 2, Tag: "two", Protocol: "trojan"},
		}}}},
		nodeUC: nodeUC,
		accountReader: &membershipAccountReader{accounts: []*contract.AccountInfo{
			{InboundID: 1, Email: "user_present"},
			{InboundID: 1, Email: "trial_missing"},
			{InboundID: 2, Email: "user_present"},
			{InboundID: 2, Email: "manual_present"},
		}},
	}
	stats, err := uc.ReconcileUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(nodeUC.added)
	if want := []string{"one:trial_missing", "two:user_present"}; !reflect.DeepEqual(nodeUC.added, want) {
		t.Errorf("added %v, want %v", nodeUC.added, want)
	}
	if want := []string{"one:user_ghost"}; !reflect.DeepEqual(nodeUC.removed, want) {
		t.Errorf("removed %v, want %v", nodeUC.removed, want)
	}
	if stats.TotalDBUsers != 4 || stats.TotalXrayUsers != 5 || stats.MissingAdded != 2 || stats.GhostsRemoved != 1 || stats.Errors != 0 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}
