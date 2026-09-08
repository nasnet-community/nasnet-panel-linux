package xray

import (
	"context"
	"net"
	"testing"
	"time"

	hs "github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/proxy/vless"
	"google.golang.org/grpc"
)

type reverseUserServer struct {
	hs.UnimplementedHandlerServiceServer
	users chan *vless.Account
}

func (s *reverseUserServer) AlterInbound(_ context.Context, req *hs.AlterInboundRequest) (*hs.AlterInboundResponse, error) {
	operation, err := req.Operation.GetInstance()
	if err != nil {
		return nil, err
	}
	account, err := operation.(*hs.AddUserOperation).User.Account.GetInstance()
	if err != nil {
		return nil, err
	}
	s.users <- account.(*vless.Account)
	return &hs.AlterInboundResponse{}, nil
}
func TestReversePermissionForAPIAddedUsers(t *testing.T) {
	tag, err := ReverseTagFromConfig([]byte(`{"xcoreHub":{"reversePortals":{"tunnel":"portal"}}}`), "tunnel")
	if err != nil || tag != "portal" {
		t.Fatalf("metadata: %s %v", tag, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	defer server.Stop()
	capture := &reverseUserServer{users: make(chan *vless.Account, 2)}
	hs.RegisterHandlerServiceServer(server, capture)
	go server.Serve(listener)
	client := NewLocalClient(listener.Addr().String(), time.Second)
	if err = client.AddUser(context.Background(), "tunnel", "user@test", "test-user", "vless", "", "", 0, tag); err != nil {
		t.Fatal(err)
	}
	if account := <-capture.users; account.Reverse.GetTag() != "portal" {
		t.Fatalf("API user lost reverse permission: %v", account)
	}
	if err = client.AddUser(context.Background(), "ordinary", "user@test", "test-user", "vless", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if account := <-capture.users; account.Reverse != nil {
		t.Fatal("ordinary user gained reverse permission")
	}
	for _, config := range []string{`{}`, `{"xcoreHub":{"reversePortals":{"other":"portal"}}}`} {
		tag, err = ReverseTagFromConfig([]byte(config), "tunnel")
		if err != nil || tag != "" {
			t.Fatal("metadata permission leaked to another inbound")
		}
	}
	if _, err = ReverseTagFromConfig([]byte(`invalid`), "tunnel"); err == nil {
		t.Fatal("invalid metadata ignored")
	}
}
