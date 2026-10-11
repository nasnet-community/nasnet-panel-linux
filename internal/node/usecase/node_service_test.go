package usecase

import (
	"context"
	"testing"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
)

type serviceAccounts struct {
	accountRepo.AccountRepository
	list []*accountDomain.Account
}

func (r *serviceAccounts) ListByNodeID(context.Context, uint) ([]*accountDomain.Account, error) {
	return r.list, nil
}

type serviceNodes struct {
	repository.NodeRepository
	nodes []*domain.Node
}

func (r *serviceNodes) ListNodes(context.Context) ([]*domain.Node, error) { return r.nodes, nil }
func (r *serviceNodes) GetNode(_ context.Context, id uint) (*domain.Node, error) {
	for _, n := range r.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func TestNodeUsersOnlyWhileInService(t *testing.T) {
	in := &domain.Inbound{Tag: "vless-in", Protocol: "vless"}
	accounts := &serviceAccounts{list: []*accountDomain.Account{
		{Email: "a@x", UUID: "u1", Status: accountDomain.AccountStatusActive, Inbound: in},
	}}
	uc := &nodeUsecase{accountRepo: accounts}

	users, err := uc.nodeUsers(context.Background(), &domain.Node{ID: 3, IsActive: true})
	if err != nil || len(users["vless-in"]) != 1 {
		t.Fatalf("in service: users=%v err=%v", users, err)
	}
	users, err = uc.nodeUsers(context.Background(), &domain.Node{ID: 3, IsActive: false})
	if err != nil || len(users) != 0 {
		t.Fatalf("out of service: users=%v err=%v", users, err)
	}
}

func TestLeftoverUsersRetriedUntilTheNodeAnswers(t *testing.T) {
	// a reverse node with no registry can't be reached, so every push fails fast
	off := &domain.Node{ID: 5, Name: "off", ConnectMode: domain.ConnectModeReverse, IsActive: false}
	older := &domain.Node{ID: 6, Name: "older", ConnectMode: domain.ConnectModeReverse, IsActive: false}
	on := &domain.Node{ID: 7, Name: "on", ConnectMode: domain.ConnectModeReverse, IsActive: true}
	uc := &nodeUsecase{
		nodeRepo:  &serviceNodes{nodes: []*domain.Node{off, older, on}},
		usersLeft: map[uint]bool{},
		pushState: map[uint]*configPushState{},
	}
	ctx := context.Background()

	uc.applyService(ctx, off)
	if !uc.UsersLeftOn(5) {
		t.Fatal("a failed push out of service must leave the node marked")
	}

	// a plain retry only touches nodes it knows about
	uc.RemoveLeftoverUsers(ctx, false)
	if !uc.UsersLeftOn(5) || uc.UsersLeftOn(6) {
		t.Fatalf("plain retry: left=%v", uc.usersLeft)
	}

	// a fresh start tries every node out of service
	uc.RemoveLeftoverUsers(ctx, true)
	if !uc.UsersLeftOn(6) || uc.UsersLeftOn(7) {
		t.Fatalf("startup retry: left=%v", uc.usersLeft)
	}

	// back in service: nothing left to remove
	off.IsActive = true
	uc.RemoveLeftoverUsers(ctx, false)
	if uc.UsersLeftOn(5) {
		t.Fatal("a node back in service is no longer marked")
	}
}
