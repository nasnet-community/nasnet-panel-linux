package usecase

import (
	"context"
	"errors"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"strings"
	"testing"
)

type loggingSettingsRepo struct {
	repository.NodeRepository
	saved *domain.XrayLogSettings
	err   error
}

func (r *loggingSettingsRepo) UpdateNodeLogSettings(_ context.Context, _ uint, settings domain.XrayLogSettings) error {
	if r.err != nil {
		return r.err
	}
	r.saved = &settings
	return nil
}

type loggingSettingsAgent struct {
	agent.NodeClient
	pushed bool
	err    error
}

func (a *loggingSettingsAgent) PushConfig(context.Context, string) error {
	a.pushed = true
	return a.err
}
func TestLoggingSettingsPersistence(t *testing.T) {
	cases := []struct {
		name, config     string
		pushErr, saveErr error
		wantErr          string
		pushed, saved    bool
	}{
		{name: "accepted", config: `{"log":{"loglevel":"debug","access":"","error":"/tmp/error","dnsLog":true}}`, pushed: true, saved: true},
		{name: "rejected by agent", config: `{"log":{"loglevel":"debug"}}`, pushErr: errors.New("invalid routing"), wantErr: "invalid routing", pushed: true},
		{name: "database failure", config: `{"log":{"loglevel":"debug"}}`, saveErr: errors.New("database down"), wantErr: "could not be persisted", pushed: true},
		{name: "invalid JSON", config: `null`, wantErr: "expected a JSON object"},
		{name: "invalid level", config: `{"log":{"loglevel":"verbose"}}`, wantErr: "invalid Xray log level"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &loggingSettingsRepo{err: tc.saveErr}
			client := &loggingSettingsAgent{err: tc.pushErr}
			uc := &nodeUsecase{nodeRepo: repo}
			node := &domain.Node{ID: 6, Name: "keep", LogLevel: "warning", LogAccess: "/old", EnableAccessLog: true}
			err := uc.applyXrayConfigAndLogs(context.Background(), node, tc.config, client)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("unexpected error %v", err)
			}
			if client.pushed != tc.pushed || (repo.saved != nil) != tc.saved {
				t.Fatalf("push=%v save=%v", client.pushed, repo.saved != nil)
			}
			if node.LogLevel != "warning" {
				t.Fatal("mutated original node before persistence")
			}
			if tc.saved && (*repo.saved.Level != "debug" || *repo.saved.Access != "" || !*repo.saved.DNS) {
				t.Fatalf("incorrect persisted settings %+v", repo.saved)
			}
		})
	}
}
