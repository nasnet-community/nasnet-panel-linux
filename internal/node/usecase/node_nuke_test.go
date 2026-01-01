package usecase

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// fakeNodeRepo embeds the full NodeRepository interface so unstubbed
// methods panic loudly on accidental call.
type fakeNodeRepo struct {
	repository.NodeRepository // nil; panics on any unimplemented call

	mu      sync.Mutex
	nodes   map[uint]*domain.Node
	deleted map[uint]bool
}

func newFakeNodeRepo(nodes ...*domain.Node) *fakeNodeRepo {
	r := &fakeNodeRepo{
		nodes:   map[uint]*domain.Node{},
		deleted: map[uint]bool{},
	}
	for _, n := range nodes {
		r.nodes[n.ID] = n
	}
	return r
}

func (r *fakeNodeRepo) GetNode(_ context.Context, id uint) (*domain.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return n, nil
}

func (r *fakeNodeRepo) UpdateNode(_ context.Context, n *domain.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[n.ID] = n
	return nil
}

func (r *fakeNodeRepo) DeleteNode(_ context.Context, id uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted[id] = true
	delete(r.nodes, id)
	return nil
}

// ListInboundsByNode + children — DeleteNode(force) walks these before the
// actual DB delete. Returning empty slices keeps the cascade a no-op.
func (r *fakeNodeRepo) ListInboundsByNode(_ context.Context, _ uint) ([]*domain.Inbound, error) {
	return nil, nil
}
func (r *fakeNodeRepo) DeleteReverseProxiesByNode(_ context.Context, _ uint) error { return nil }
func (r *fakeNodeRepo) DeleteOutboundsByNode(_ context.Context, _ uint) error      { return nil }
func (r *fakeNodeRepo) DeleteRoutingRulesByNode(_ context.Context, _ uint) error   { return nil }
func (r *fakeNodeRepo) DeleteBalancingRulesByNode(_ context.Context, _ uint) error { return nil }
func (r *fakeNodeRepo) DeleteNodeStatsByNode(_ context.Context, _ uint) error      { return nil }

// fakeAgentClient embeds agent.NodeClient; only Wipe/Nuke/Close/Target
// are stubbed. Other calls nil-panic loudly.

type fakeAgentClient struct {
	agent.NodeClient // nil

	wipeResp *pb.NukeReport
	wipeErr  error
	wipeHook func(*pb.NukeRequest) // optional: observe requests

	nukeResp *fakeNukeStream
	nukeErr  error

	closed bool
}

func (f *fakeAgentClient) Wipe(_ context.Context, req *pb.NukeRequest) (*pb.NukeReport, error) {
	if f.wipeHook != nil {
		f.wipeHook(req)
	}
	if f.wipeErr != nil {
		return nil, f.wipeErr
	}
	return f.wipeResp, nil
}

func (f *fakeAgentClient) Nuke(_ context.Context, _ *pb.NukeRequest) (pb.NodeAgent_NukeClient, error) {
	if f.nukeErr != nil {
		return nil, f.nukeErr
	}
	return f.nukeResp, nil
}

func (f *fakeAgentClient) Close() error   { f.closed = true; return nil }
func (f *fakeAgentClient) Target() string { return "fake" }

// fakeNukeStream implements grpc.ServerStreamingClient[NukeProgress].
type fakeNukeStream struct {
	msgs []*pb.NukeProgress
	idx  int
	err  error // returned before EOF, if set
}

func (s *fakeNukeStream) Recv() (*pb.NukeProgress, error) {
	if s.idx >= len(s.msgs) {
		if s.err != nil {
			err := s.err
			s.err = nil
			return nil, err
		}
		return nil, io.EOF
	}
	m := s.msgs[s.idx]
	s.idx++
	return m, nil
}

// ClientStream plumbing — tests don't exercise these, but the interface
// demands them.
func (s *fakeNukeStream) Header() (metadata.MD, error) { return nil, nil }
func (s *fakeNukeStream) Trailer() metadata.MD         { return nil }
func (s *fakeNukeStream) CloseSend() error             { return nil }
func (s *fakeNukeStream) Context() context.Context     { return context.Background() }
func (s *fakeNukeStream) SendMsg(_ interface{}) error  { return nil }
func (s *fakeNukeStream) RecvMsg(_ interface{}) error  { return nil }

var _ grpc.ServerStreamingClient[pb.NukeProgress] = (*fakeNukeStream)(nil)

// ── blockingFake ────────────────────────────────────────────────────────────
// fakeAgentClient variant that parks Wipe on a channel so tests can observe a
// second in-flight call being rejected.

type blockingFake struct {
	agent.NodeClient

	started  chan struct{}
	release  chan struct{}
	wipeResp *pb.NukeReport
}

func (b *blockingFake) Wipe(_ context.Context, _ *pb.NukeRequest) (*pb.NukeReport, error) {
	close(b.started)
	<-b.release
	return b.wipeResp, nil
}

func (b *blockingFake) Nuke(_ context.Context, _ *pb.NukeRequest) (pb.NodeAgent_NukeClient, error) {
	return nil, errors.New("blockingFake: Nuke not supported")
}

func (b *blockingFake) Close() error   { return nil }
func (b *blockingFake) Target() string { return "blocking-fake" }

// ── test helper ─────────────────────────────────────────────────────────────

func newNodeUsecaseForTest(node *domain.Node, client agent.NodeClient) *nodeUsecase {
	repo := newFakeNodeRepo(node)
	u := &nodeUsecase{
		nodeRepo:      repo,
		pushState:     map[uint]*configPushState{},
		nukesInFlight: map[uint]struct{}{},
	}
	u.nukeAgentClientFactory = func(_ context.Context, _ *domain.Node) (agent.NodeClient, error) {
		return client, nil
	}
	return u
}

// newNodeUsecaseForTestWithRepo is like newNodeUsecaseForTest but returns the
// fake repo too so tests can assert on its side-effects (e.g. whether
// DeleteNode was called).
func newNodeUsecaseForTestWithRepo(node *domain.Node, client agent.NodeClient) (*nodeUsecase, *fakeNodeRepo) {
	repo := newFakeNodeRepo(node)
	u := &nodeUsecase{
		nodeRepo:      repo,
		pushState:     map[uint]*configPushState{},
		nukesInFlight: map[uint]struct{}{},
	}
	u.nukeAgentClientFactory = func(_ context.Context, _ *domain.Node) (agent.NodeClient, error) {
		return client, nil
	}
	return u, repo
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestNodeUC_Nuke_WipeModeEmitsPerPhaseFrames(t *testing.T) {
	phases := []*pb.NukePhaseResult{
		{Phase: "stop_xray", Ok: true, DurationMs: 10},
		{Phase: "wipe_xray", Ok: true, DurationMs: 20},
		{Phase: "wipe_wireguard", Ok: true, DurationMs: 30},
	}
	fake := &fakeAgentClient{wipeResp: &pb.NukeReport{
		Mode:            "NUKE_MODE_WIPE",
		Phases:          phases,
		Result:          pb.NukeReport_NUKE_RESULT_SUCCESS,
		TotalDurationMs: 60,
	}}
	node := &domain.Node{ID: 42, Name: "test", ConnectMode: "direct", IsActive: true}
	uc := newNodeUsecaseForTest(node, fake)

	var streamed []string
	emit := func(p *pb.NukePhaseResult) { streamed = append(streamed, p.Phase) }

	report, err := uc.Nuke(context.Background(), 42, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_WIPE,
		KeepHubRecord: true,
	}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 3 {
		t.Fatalf("expected 3 synthetic frames, got %d", len(streamed))
	}
	if report.Result != pb.NukeReport_NUKE_RESULT_SUCCESS {
		t.Fatalf("unexpected result: %v", report.Result)
	}
	if !fake.closed {
		t.Error("expected agent client to be closed after Nuke returns")
	}
}

func TestNodeUC_Nuke_NukeModeStreamsThroughNukeRPC(t *testing.T) {
	phases := []*pb.NukePhaseResult{
		{Phase: "stop_xray", Ok: true},
		{Phase: "shred_root", Ok: true},
	}
	finalReport := &pb.NukeReport{
		Mode:            "NUKE_MODE_NUKE",
		Phases:          phases,
		Result:          pb.NukeReport_NUKE_RESULT_SUCCESS,
		TotalDurationMs: 123,
	}
	stream := &fakeNukeStream{msgs: []*pb.NukeProgress{
		{Event: &pb.NukeProgress_Phase{Phase: phases[0]}},
		{Event: &pb.NukeProgress_Phase{Phase: phases[1]}},
		{Event: &pb.NukeProgress_Done{Done: finalReport}},
	}}
	fake := &fakeAgentClient{
		nukeResp: stream,
		wipeErr:  errors.New("Wipe must not be called on direct-mode NUKE"),
	}
	node := &domain.Node{ID: 21, Name: "direct-nuke", ConnectMode: "direct", IsActive: true}
	uc := newNodeUsecaseForTest(node, fake)

	var streamed []string
	emit := func(p *pb.NukePhaseResult) { streamed = append(streamed, p.Phase) }

	report, err := uc.Nuke(context.Background(), 21, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_NUKE,
		KeepHubRecord: true,
	}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 2 {
		t.Fatalf("expected 2 streamed frames, got %d", len(streamed))
	}
	if report.TotalDurationMs != 123 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestNodeUC_Nuke_SetsNukedAtWhenKeepHubRecord(t *testing.T) {
	fake := &fakeAgentClient{
		wipeResp: &pb.NukeReport{Result: pb.NukeReport_NUKE_RESULT_SUCCESS},
	}
	node := &domain.Node{ID: 9, Name: "keep", ConnectMode: "direct", IsActive: true, IsOnline: true}
	uc := newNodeUsecaseForTest(node, fake)

	before := time.Now().Add(-time.Second)
	_, err := uc.Nuke(context.Background(), 9, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_WIPE,
		KeepHubRecord: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if node.NukedAt == nil || node.NukedAt.Before(before) {
		t.Fatalf("NukedAt not set: %v", node.NukedAt)
	}
	if node.NukeMode != "WIPE" {
		t.Fatalf("expected WIPE, got %q", node.NukeMode)
	}
	if node.IsActive {
		t.Fatal("IsActive should be false after wipe")
	}
	if node.IsOnline {
		t.Fatal("IsOnline should be false after wipe")
	}
}

func TestNodeUC_Nuke_PartialResultMarksNukePartial(t *testing.T) {
	// Mode=NUKE goes through the streaming RPC. When the agent reports PARTIAL,
	// the hub record must be annotated NUKE_PARTIAL rather than plain NUKE,
	// because the operator asked for NUKE but only some phases finished.
	stream := &fakeNukeStream{msgs: []*pb.NukeProgress{
		{Event: &pb.NukeProgress_Done{Done: &pb.NukeReport{
			Mode:   "NUKE_MODE_NUKE",
			Result: pb.NukeReport_NUKE_RESULT_PARTIAL,
		}}},
	}}
	fake := &fakeAgentClient{
		nukeResp: stream,
		wipeErr:  errors.New("Wipe must not be called for NUKE mode"),
	}
	node := &domain.Node{ID: 13, Name: "partial", ConnectMode: "direct", IsActive: true}
	uc := newNodeUsecaseForTest(node, fake)

	_, err := uc.Nuke(context.Background(), 13, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_NUKE,
		KeepHubRecord: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if node.NukeMode != "NUKE_PARTIAL" {
		t.Fatalf("expected NUKE_PARTIAL, got %q", node.NukeMode)
	}
}

func TestNodeUC_Nuke_DryRunLeavesRecordUntouched(t *testing.T) {
	fake := &fakeAgentClient{
		wipeResp: &pb.NukeReport{Result: pb.NukeReport_NUKE_RESULT_SUCCESS, DryRun: true},
	}
	node := &domain.Node{ID: 17, Name: "dry", ConnectMode: "direct", IsActive: true, IsOnline: true}
	uc := newNodeUsecaseForTest(node, fake)

	_, err := uc.Nuke(context.Background(), 17, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_WIPE,
		KeepHubRecord: true,
		DryRun:        true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if node.NukedAt != nil {
		t.Fatalf("dry-run must not set NukedAt, got %v", node.NukedAt)
	}
	if !node.IsActive {
		t.Fatal("dry-run must leave IsActive untouched")
	}
}

func TestNodeUC_Nuke_ConcurrentCallReturnsErrNukeInFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	node := &domain.Node{ID: 11, Name: "busy", ConnectMode: "direct", IsActive: true}
	blocking := &blockingFake{
		started:  started,
		release:  release,
		wipeResp: &pb.NukeReport{Result: pb.NukeReport_NUKE_RESULT_SUCCESS},
	}
	uc := newNodeUsecaseForTest(node, blocking)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = uc.Nuke(context.Background(), 11, NukeOptions{
			Mode:          pb.NukeMode_NUKE_MODE_WIPE,
			KeepHubRecord: true,
		}, nil)
	}()
	<-started

	_, err := uc.Nuke(context.Background(), 11, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_WIPE,
		KeepHubRecord: true,
	}, nil)
	if !errors.Is(err, ErrNukeInFlight) {
		t.Fatalf("expected ErrNukeInFlight, got %v", err)
	}
	close(release)
	<-firstDone
}

func TestNodeUC_Nuke_FailedResultLeavesRecordIntact(t *testing.T) {
	fake := &fakeAgentClient{wipeResp: &pb.NukeReport{
		Mode:   "NUKE_MODE_WIPE",
		Phases: []*pb.NukePhaseResult{{Phase: "stop_xray", Ok: false, Error: "boom"}},
		Result: pb.NukeReport_NUKE_RESULT_FAILED,
	}}
	node := &domain.Node{ID: 99, Name: "failnode", ConnectMode: "direct", IsActive: true}
	uc := newNodeUsecaseForTest(node, fake)

	_, err := uc.Nuke(context.Background(), 99, NukeOptions{
		Mode: pb.NukeMode_NUKE_MODE_WIPE, KeepHubRecord: true,
	}, nil)
	if !errors.Is(err, ErrNukeFailed) {
		t.Fatalf("expected ErrNukeFailed, got %v", err)
	}
	if node.NukedAt != nil {
		t.Fatalf("NukedAt must not be set on FAILED: %v", node.NukedAt)
	}
	if node.NukeMode != "" {
		t.Fatalf("NukeMode must be empty on FAILED: %q", node.NukeMode)
	}
	if !node.IsActive {
		t.Fatalf("IsActive must remain true on FAILED")
	}
}

func TestNodeUC_Nuke_FailedResultDoesNotDeleteRecord(t *testing.T) {
	// KeepHubRecord=false normally cascade-deletes, but FAILED should skip that.
	// Direct-mode NUKE routes through the streaming RPC, so the FAILED result
	// must come back as the final Done frame rather than a Wipe reply.
	finalReport := &pb.NukeReport{
		Mode:   "NUKE_MODE_NUKE",
		Result: pb.NukeReport_NUKE_RESULT_FAILED,
	}
	stream := &fakeNukeStream{msgs: []*pb.NukeProgress{
		{Event: &pb.NukeProgress_Done{Done: finalReport}},
	}}
	fake := &fakeAgentClient{nukeResp: stream}
	node := &domain.Node{ID: 100, Name: "failnuke", ConnectMode: "direct", IsActive: true}
	uc, repo := newNodeUsecaseForTestWithRepo(node, fake)

	_, err := uc.Nuke(context.Background(), 100, NukeOptions{
		Mode: pb.NukeMode_NUKE_MODE_NUKE, KeepHubRecord: false,
	}, nil)
	if !errors.Is(err, ErrNukeFailed) {
		t.Fatalf("expected ErrNukeFailed, got %v", err)
	}
	// Node must still exist in the repo; DeleteNode cascade must not fire.
	repo.mu.Lock()
	deleted := repo.deleted[100]
	_, stillPresent := repo.nodes[100]
	repo.mu.Unlock()
	if deleted {
		t.Fatal("node must not be cascade-deleted on FAILED")
	}
	if !stillPresent {
		t.Fatal("node record must remain in repo on FAILED")
	}
	if !node.IsActive {
		t.Fatalf("IsActive must remain true on FAILED")
	}
}

func TestNodeUC_Nuke_StreamErrorBubblesUp(t *testing.T) {
	stream := &fakeNukeStream{
		msgs: []*pb.NukeProgress{},
		err:  errors.New("transient gRPC error"),
	}
	fake := &fakeAgentClient{nukeResp: stream}
	node := &domain.Node{ID: 33, Name: "bad-stream", ConnectMode: "direct", IsActive: true}
	uc := newNodeUsecaseForTest(node, fake)

	_, err := uc.Nuke(context.Background(), 33, NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_NUKE,
		KeepHubRecord: true,
	}, nil)
	if err == nil {
		t.Fatal("expected error from broken stream, got nil")
	}
	// Record must NOT have been marked nuked when the remote run failed to
	// return a final report.
	if node.NukedAt != nil {
		t.Fatal("failed run must not set NukedAt")
	}
}
