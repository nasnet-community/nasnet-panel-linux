package starlink

import (
	"context"
	"fmt"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"
)

type SelfTestResult struct {
	Passed bool   `json:"passed"`
	Report string `json:"report"`
}

func (c *Client) do(ctx context.Context, name string, req *pb.Request) (*pb.Response, error) {
	resp, err := c.handle(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return resp, nil
}

func (c *Client) Reboot(ctx context.Context) error {
	_, err := c.do(ctx, "reboot", &pb.Request{Request: &pb.Request_Reboot{Reboot: &pb.RebootRequest{}}})
	return err
}

func (c *Client) Stow(ctx context.Context, unstow bool) error {
	_, err := c.do(ctx, "stow", &pb.Request{Request: &pb.Request_DishStow{DishStow: &pb.DishStowRequest{Unstow: unstow}}})
	return err
}

func (c *Client) ClearObstructionMap(ctx context.Context) error {
	_, err := c.do(ctx, "clear obstruction map", &pb.Request{
		Request: &pb.Request_DishClearObstructionMap{DishClearObstructionMap: &pb.DishClearObstructionMapRequest{}},
	})
	return err
}

func (c *Client) InhibitGPS(ctx context.Context, inhibit bool) error {
	_, err := c.do(ctx, "inhibit gps", &pb.Request{
		Request: &pb.Request_DishInhibitGps{DishInhibitGps: &pb.DishInhibitGpsRequest{InhibitGps: inhibit}},
	})
	return err
}

func (c *Client) FactoryReset(ctx context.Context) error {
	_, err := c.do(ctx, "factory reset", &pb.Request{
		Request: &pb.Request_DishFactoryReset{DishFactoryReset: &pb.DishFactoryResetRequest{}},
	})
	return err
}

func (c *Client) SelfTest(ctx context.Context) (*SelfTestResult, error) {
	resp, err := c.do(ctx, "self test", &pb.Request{Request: &pb.Request_SelfTest{SelfTest: &pb.SelfTestRequest{Detailed: true}}})
	if err != nil {
		return nil, err
	}
	st := resp.GetSelfTest()
	if st == nil {
		return &SelfTestResult{}, nil
	}
	return &SelfTestResult{Passed: st.Passed, Report: st.Report}, nil
}
